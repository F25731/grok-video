package app

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
)

func (s *Server) parseVideoRequest(r *http.Request) (videoRequest, error) {
	contentType := r.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "multipart/form-data") {
		return s.parseMultipartVideoRequest(r, contentType)
	}
	var raw map[string]any
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&raw); err != nil {
		return videoRequest{}, fmt.Errorf("invalid json body")
	}
	return videoRequestFromMap(raw), nil
}

func (s *Server) parseMultipartVideoRequest(r *http.Request, contentType string) (videoRequest, error) {
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return videoRequest{}, fmt.Errorf("invalid multipart content type")
	}
	reader := multipart.NewReader(r.Body, params["boundary"])
	form, err := reader.ReadForm(s.cfg.MaxImageBytes)
	if err != nil {
		return videoRequest{}, err
	}
	defer form.RemoveAll()

	req := videoRequest{
		Model:       formValue(form, "model"),
		Prompt:      formValue(form, "prompt"),
		AspectRatio: firstNonEmpty(formValue(form, "aspect_ratio"), formValue(form, "aspectRatio"), formValue(form, "size")),
		Resolution:  formValue(form, "resolution"),
		Seconds:     intValue(firstNonEmpty(formValue(form, "seconds"), formValue(form, "duration"))),
	}
	for _, key := range []string{"image_urls", "imageUrls", "images", "image", "input_reference", "reference_images", "referenceImages"} {
		for _, value := range form.Value[key] {
			req.ImageURLs = append(req.ImageURLs, imageURLsFromString(value)...)
		}
	}
	for _, files := range form.File {
		for _, fh := range files {
			url, err := fileToDataURL(fh, s.cfg.MaxImageBytes)
			if err != nil {
				return videoRequest{}, err
			}
			req.ImageURLs = append(req.ImageURLs, url)
		}
	}
	req.ImageURLs = compactStrings(req.ImageURLs)
	return req, nil
}

func videoRequestFromMap(raw map[string]any) videoRequest {
	req := videoRequest{
		Model:       stringValue(raw, "model"),
		Prompt:      stringValue(raw, "prompt"),
		AspectRatio: firstNonEmpty(stringValue(raw, "aspect_ratio"), stringValue(raw, "aspectRatio"), stringValue(raw, "size")),
		Resolution:  stringValue(raw, "resolution"),
		Seconds:     numberValue(firstNonNil(raw["seconds"], raw["duration"])),
	}
	for _, key := range []string{"image_urls", "imageUrls", "images", "image", "input_reference", "reference_images", "referenceImages"} {
		req.ImageURLs = append(req.ImageURLs, imageURLsFromAny(raw[key])...)
	}
	req.ImageURLs = compactStrings(req.ImageURLs)
	return req
}

func validateVideoRequest(req *videoRequest) error {
	req.Model = strings.TrimSpace(req.Model)
	req.Prompt = strings.TrimSpace(req.Prompt)
	if req.Model == "" {
		return fmt.Errorf("model field is required")
	}
	spec, ok := modelSpecs()[req.Model]
	if !ok {
		return fmt.Errorf("unsupported model %q", req.Model)
	}
	if req.Prompt == "" {
		return fmt.Errorf("prompt is required")
	}
	if req.Seconds <= 0 {
		req.Seconds = 4
	}
	if req.Seconds > spec.MaxSeconds {
		req.Seconds = spec.MaxSeconds
	}
	if len(req.ImageURLs) > 1 && spec.MultiMaxSec > 0 && req.Seconds > spec.MultiMaxSec {
		req.Seconds = spec.MultiMaxSec
	}
	if req.AspectRatio == "" {
		req.AspectRatio = "16:9"
	}
	if !spec.Ratios[req.AspectRatio] {
		return fmt.Errorf("aspect_ratio %q is not supported by %s", req.AspectRatio, req.Model)
	}
	if req.Resolution == "" {
		req.Resolution = "720p"
	}
	if req.Resolution != "720p" && req.Resolution != "480p" {
		return fmt.Errorf("resolution must be 720p or 480p")
	}
	if spec.RequireImage && len(req.ImageURLs) != 1 {
		return fmt.Errorf("%s only supports exactly one reference image", req.Model)
	}
	if !spec.TextToVideo && len(req.ImageURLs) == 0 {
		return fmt.Errorf("%s requires one reference image", req.Model)
	}
	if len(req.ImageURLs) > spec.MaxImages {
		return fmt.Errorf("%s supports at most %d reference images", req.Model, spec.MaxImages)
	}
	return nil
}

func fileToDataURL(fh *multipart.FileHeader, maxBytes int64) (string, error) {
	file, err := fh.Open()
	if err != nil {
		return "", err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return "", err
	}
	if int64(len(data)) > maxBytes {
		return "", fmt.Errorf("image file is too large")
	}
	contentType := fh.Header.Get("Content-Type")
	if contentType == "" || contentType == "application/octet-stream" {
		contentType = http.DetectContentType(data)
	}
	return "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

func imageURLsFromAny(value any) []string {
	switch item := value.(type) {
	case string:
		return imageURLsFromString(item)
	case []any:
		out := make([]string, 0, len(item))
		for _, child := range item {
			out = append(out, imageURLsFromAny(child)...)
		}
		return out
	case map[string]any:
		for _, key := range []string{"image_url", "url", "image", "input_image", "imageUrls", "image_urls"} {
			if urls := imageURLsFromAny(item[key]); len(urls) > 0 {
				return urls
			}
		}
	}
	return nil
}

func imageURLsFromString(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if strings.HasPrefix(value, "{") || strings.HasPrefix(value, "[") {
		var decoded any
		if json.Unmarshal([]byte(value), &decoded) == nil {
			return imageURLsFromAny(decoded)
		}
	}
	return []string{value}
}

func compactStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func formValue(form *multipart.Form, key string) string {
	if values := form.Value[key]; len(values) > 0 {
		return strings.TrimSpace(values[0])
	}
	return ""
}

func stringValue(raw map[string]any, key string) string {
	value, _ := raw[key].(string)
	return strings.TrimSpace(value)
}

func numberValue(value any) int {
	switch item := value.(type) {
	case float64:
		return int(item)
	case int:
		return item
	case string:
		return intValue(item)
	}
	return 0
}

func intValue(value string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(value))
	return n
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
