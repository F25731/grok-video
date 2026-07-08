package app

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"
)

type openAIImageOutput struct {
	URL string `json:"url,omitempty"`
}

func (s *Server) imageGeneration(w http.ResponseWriter, r *http.Request) {
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
	s.runImageBilledVideo(w, r, req)
}

func (s *Server) runImageBilledVideo(w http.ResponseWriter, r *http.Request, req videoRequest) {
	if err := validateVideoRequest(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	heartbeat := startJSONHeartbeat(w, 10*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.RequestTimeout)
	defer cancel()

	outputs := make([]openAIImageOutput, 0, req.N)
	var mu sync.Mutex
	var wg sync.WaitGroup
	errCh := make(chan error, req.N)
	for i := 0; i < req.N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := s.pool.Run(ctx, func() error {
				url, err := s.createAndWaitVideo(ctx, req)
				if err != nil {
					return err
				}
				mu.Lock()
				outputs = append(outputs, openAIImageOutput{URL: url})
				mu.Unlock()
				return nil
			})
			if err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	if err := <-errCh; err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, errQueueFull) {
			status = http.StatusTooManyRequests
		}
		writeJSONWithHeartbeat(w, heartbeat, status, openAIErrorPayload(err.Error()))
		return
	}
	writeJSONWithHeartbeat(w, heartbeat, http.StatusOK, map[string]any{
		"created": time.Now().Unix(),
		"data":    outputs,
	})
}

func (s *Server) createAndWaitVideo(ctx context.Context, req videoRequest) (string, error) {
	created, _, err := s.client.Create(ctx, req)
	if err != nil {
		return "", err
	}
	video := toOpenAIFromCreate(req, created)
	s.tasks.Create(video)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		task, _, err := s.client.Poll(ctx, created.TaskID.String())
		if err == nil {
			video = toOpenAIFromTask(task, req.Model)
			s.tasks.Update(video)
			if video.Status == "completed" {
				if url, ok := video.Metadata["video_url"].(string); ok && url != "" {
					return url, nil
				}
				return "", errors.New("upstream completed without video url")
			}
			if video.Status == "failed" {
				if video.Error != nil && video.Error.Message != "" {
					return "", errors.New(video.Error.Message)
				}
				return "", errors.New("upstream task failed")
			}
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
		}
	}
}

func openAIErrorPayload(message string) map[string]any {
	return map[string]any{
		"error": map[string]string{
			"message": message,
			"type":    "invalid_request_error",
			"code":    "upstream_failed",
		},
	}
}
