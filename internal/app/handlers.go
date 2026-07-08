package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func (s *Server) createVideo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.runtime.Get().UpstreamAPIKey == "" {
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
	var created upstreamCreateResp
	err = s.pool.Run(ctx, func() error {
		var createErr error
		created, _, createErr = s.client.Create(ctx, req)
		return createErr
	})
	if err != nil {
		writePoolError(w, err)
		return
	}
	video := toOpenAIFromCreate(req, created)
	s.tasks.Create(video)
	go s.watchTask(created.TaskID.String())
	writeJSON(w, http.StatusOK, video)
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
	var task upstreamTask
	err := s.pool.Run(ctx, func() error {
		var pollErr error
		task, _, pollErr = s.client.Poll(ctx, taskID)
		return pollErr
	})
	if err != nil {
		writePoolError(w, err)
		return
	}
	video := toOpenAIFromTask(task, s.modelForTask(taskID))
	s.tasks.Update(video)
	writeJSON(w, http.StatusOK, video)
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
	var task upstreamTask
	err := s.pool.Run(ctx, func() error {
		var pollErr error
		task, _, pollErr = s.client.Poll(ctx, taskID)
		return pollErr
	})
	if err != nil {
		writePoolError(w, err)
		return
	}
	if strings.ToUpper(task.Status) != "SUCCESS" || strings.TrimSpace(task.ResultURL) == "" {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("task is not completed, current status: %s", task.Status))
		return
	}
	var resp *http.Response
	err = s.pool.Run(ctx, func() error {
		var fetchErr error
		resp, fetchErr = s.client.FetchContent(ctx, task.ResultURL)
		return fetchErr
	})
	if err != nil {
		writePoolError(w, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("video url returned status %d", resp.StatusCode))
		return
	}
	s.tasks.SetContentFetched(taskID)
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

func writePoolError(w http.ResponseWriter, err error) {
	if errors.Is(err, errQueueFull) {
		writeError(w, http.StatusTooManyRequests, err.Error())
		return
	}
	writeError(w, http.StatusBadGateway, err.Error())
}

func (s *Server) modelForTask(taskID string) string {
	return s.tasks.ModelFor(taskID)
}

func (s *Server) watchTask(taskID string) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	deadline := time.After(s.cfg.RequestTimeout)
	for {
		select {
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), s.cfg.HTTPTimeout)
			task, _, err := s.client.Poll(ctx, taskID)
			cancel()
			if err != nil {
				continue
			}
			video := toOpenAIFromTask(task, s.modelForTask(taskID))
			s.tasks.Update(video)
			if video.Status == "completed" || video.Status == "failed" {
				return
			}
		case <-deadline:
			return
		}
	}
}
