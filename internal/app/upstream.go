package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type UpstreamClient struct {
	cfg        Config
	httpClient *http.Client
}

func NewUpstreamClient(cfg Config) *UpstreamClient {
	return &UpstreamClient{cfg: cfg, httpClient: &http.Client{Timeout: cfg.HTTPTimeout}}
}

func (c *UpstreamClient) Create(ctx context.Context, req videoRequest) (upstreamCreateResp, []byte, error) {
	payload := map[string]any{
		"model":        req.Model,
		"prompt":       req.Prompt,
		"seconds":      req.Seconds,
		"aspect_ratio": req.AspectRatio,
		"resolution":   req.Resolution,
	}
	if len(req.ImageURLs) > 0 {
		payload["image_urls"] = req.ImageURLs
	}
	body, _ := json.Marshal(payload)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.UpstreamBaseURL+"/v1/video/generations", bytes.NewReader(body))
	if err != nil {
		return upstreamCreateResp{}, nil, err
	}
	c.setHeaders(httpReq)
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return upstreamCreateResp{}, nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= http.StatusBadRequest {
		return upstreamCreateResp{}, respBody, fmt.Errorf("upstream http %d: %s", resp.StatusCode, bodyMessage(respBody))
	}
	var out upstreamCreateResp
	if err := json.Unmarshal(respBody, &out); err != nil {
		return upstreamCreateResp{}, respBody, err
	}
	if out.TaskID == "" {
		out.TaskID = out.ID
	}
	if out.ID == "" {
		out.ID = out.TaskID
	}
	if out.TaskID == "" {
		return upstreamCreateResp{}, respBody, fmt.Errorf("upstream response missing task_id")
	}
	return out, respBody, nil
}

func (c *UpstreamClient) Poll(ctx context.Context, taskID string) (upstreamTask, []byte, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.UpstreamBaseURL+"/v1/video/generations/"+taskID, nil)
	if err != nil {
		return upstreamTask{}, nil, err
	}
	c.setHeaders(httpReq)
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return upstreamTask{}, nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= http.StatusBadRequest {
		return upstreamTask{}, respBody, fmt.Errorf("upstream http %d: %s", resp.StatusCode, bodyMessage(respBody))
	}
	var out upstreamPollResp
	if err := json.Unmarshal(respBody, &out); err != nil {
		return upstreamTask{}, respBody, err
	}
	if strings.EqualFold(out.Code, "success") || out.Code == "" {
		if out.Data.TaskID == "" {
			out.Data.TaskID = taskID
		}
		return out.Data, respBody, nil
	}
	msg := firstNonEmpty(out.Message, bodyMessage(respBody))
	return upstreamTask{}, respBody, errors.New(msg)
}

func (c *UpstreamClient) FetchContent(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return c.httpClient.Do(req)
}

func (c *UpstreamClient) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.cfg.UpstreamAPIKey)
	req.Header.Set("Content-Type", "application/json")
}

func toOpenAIFromCreate(req videoRequest, created upstreamCreateResp) openAIVideo {
	progress := progressInt(created.Progress)
	return openAIVideo{
		ID:        created.TaskID,
		TaskID:    created.TaskID,
		Object:    "video",
		Model:     req.Model,
		Status:    "queued",
		Progress:  progress,
		CreatedAt: created.CreatedAtOrNow(),
		Seconds:   strconv.Itoa(req.Seconds),
		Metadata: map[string]any{
			"upstream_status": created.Status,
		},
	}
}

func toOpenAIFromTask(task upstreamTask, model string) openAIVideo {
	status := strings.ToUpper(task.Status)
	video := openAIVideo{
		ID:        task.TaskID,
		TaskID:    task.TaskID,
		Object:    "video",
		Model:     model,
		Status:    "in_progress",
		Progress:  progressPercent(task.Progress),
		CreatedAt: time.Now().Unix(),
		Metadata: map[string]any{
			"upstream_status": task.Status,
		},
	}
	switch status {
	case "SUCCESS":
		video.Status = "completed"
		video.Progress = 100
		video.CompletedAt = time.Now().Unix()
		if task.ResultURL != "" {
			video.Metadata["url"] = task.ResultURL
			video.Metadata["video_url"] = task.ResultURL
		}
	case "FAILURE", "FAILED", "TIMEOUT", "CANCELED", "CANCELLED":
		video.Status = "failed"
		video.Progress = 100
		video.CompletedAt = time.Now().Unix()
		video.Error = &videoError{Message: firstNonEmpty(task.FailReason, "task failed"), Code: "upstream_failed"}
	case "SUBMITTED", "QUEUED", "NOT_START":
		video.Status = "queued"
	case "IN_PROGRESS":
		video.Status = "in_progress"
	}
	return video
}

func (r upstreamCreateResp) CreatedAtOrNow() int64 {
	if r.CreatedAt > 0 {
		return r.CreatedAt
	}
	return time.Now().Unix()
}

func progressInt(value any) int {
	switch item := value.(type) {
	case float64:
		return int(item)
	case int:
		return item
	case string:
		return progressPercent(item)
	}
	return 0
}

func progressPercent(value string) int {
	value = strings.TrimSpace(strings.TrimSuffix(value, "%"))
	n, _ := strconv.Atoi(value)
	if n < 0 {
		return 0
	}
	if n > 100 {
		return 100
	}
	return n
}

func bodyMessage(body []byte) string {
	var raw map[string]any
	if json.Unmarshal(body, &raw) == nil {
		for _, key := range []string{"message", "msg"} {
			if value, ok := raw[key].(string); ok && value != "" {
				return value
			}
		}
		if errObj, ok := raw["error"].(map[string]any); ok {
			if value, ok := errObj["message"].(string); ok && value != "" {
				return value
			}
		}
	}
	return strings.TrimSpace(string(body))
}
