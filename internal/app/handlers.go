package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func (s *Server) createVideo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.cfg.UpstreamAPIKey == "" {
		writeError(w, http.StatusInternalServerError, "upstream api key is not configured")
		return
	}
	req, err := s.parseVideoRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateVideoRequest(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.RequestTimeout)
	defer cancel()
	created, _, err := s.client.Create(ctx, req)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	s.tasks.Store(created.TaskID, req.Model)
	writeJSON(w, http.StatusOK, toOpenAIFromCreate(req, created))
}

func (s *Server) videoGenerationByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/v1/video/generations/")
	s.pollVideo(w, r, id)
}

func (s *Server) videoByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/v1/videos/")
	if strings.HasSuffix(id, "/content") {
		id = strings.TrimSuffix(id, "/content")
		s.videoContent(w, r, id)
		return
	}
	s.pollVideo(w, r, id)
}

func (s *Server) pollVideo(w http.ResponseWriter, r *http.Request, taskID string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	taskID = strings.Trim(taskID, "/ ")
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "task_id is required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.HTTPTimeout)
	defer cancel()
	task, _, err := s.client.Poll(ctx, taskID)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toOpenAIFromTask(task, s.modelForTask(taskID)))
}

func (s *Server) videoContent(w http.ResponseWriter, r *http.Request, taskID string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	taskID = strings.Trim(taskID, "/ ")
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "task_id is required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.HTTPTimeout)
	defer cancel()
	task, _, err := s.client.Poll(ctx, taskID)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if strings.ToUpper(task.Status) != "SUCCESS" || strings.TrimSpace(task.ResultURL) == "" {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("task is not completed, current status: %s", task.Status))
		return
	}
	resp, err := s.client.FetchContent(ctx, task.ResultURL)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("video url returned status %d", resp.StatusCode))
		return
	}
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.Header().Set("Content-Type", firstNonEmpty(resp.Header.Get("Content-Type"), "video/mp4"))
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, resp.Body)
}

func (s *Server) modelForTask(taskID string) string {
	if value, ok := s.tasks.Load(taskID); ok {
		if model, ok := value.(string); ok && strings.TrimSpace(model) != "" {
			return model
		}
	}
	return "grok-video"
}
