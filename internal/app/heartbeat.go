package app

import (
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type jsonHeartbeat struct {
	w       http.ResponseWriter
	stop    chan struct{}
	done    chan struct{}
	once    sync.Once
	started atomic.Bool
}

func startJSONHeartbeat(w http.ResponseWriter, interval time.Duration) *jsonHeartbeat {
	if interval <= 0 {
		return nil
	}
	h := &jsonHeartbeat{w: w, stop: make(chan struct{}), done: make(chan struct{})}
	go h.run(interval)
	return h
}

func (h *jsonHeartbeat) run(interval time.Duration) {
	defer close(h.done)
	timer := time.NewTimer(interval)
	defer timer.Stop()
	for {
		select {
		case <-h.stop:
			return
		case <-timer.C:
			if !h.writeBlank() {
				return
			}
			timer.Reset(interval)
		}
	}
}

func (h *jsonHeartbeat) writeBlank() bool {
	if !h.started.Load() {
		header := h.w.Header()
		header.Set("Content-Type", "application/json; charset=utf-8")
		header.Set("Cache-Control", "no-cache")
		header.Set("X-Accel-Buffering", "no")
		h.w.WriteHeader(http.StatusOK)
		h.started.Store(true)
	}
	if _, err := h.w.Write([]byte("\n")); err != nil {
		return false
	}
	if flusher, ok := h.w.(http.Flusher); ok {
		flusher.Flush()
	}
	return true
}

func (h *jsonHeartbeat) stopAndStarted() bool {
	if h == nil {
		return false
	}
	h.once.Do(func() {
		close(h.stop)
	})
	<-h.done
	return h.started.Load()
}

func writeJSONWithHeartbeat(w http.ResponseWriter, h *jsonHeartbeat, status int, payload any) {
	if h == nil || !h.stopAndStarted() {
		writeJSON(w, status, payload)
		return
	}
	body, err := json.Marshal(payload)
	if err != nil {
		body, _ = json.Marshal(map[string]any{
			"error": map[string]string{
				"message": "failed to encode response",
				"type":    "invalid_request_error",
				"code":    "upstream_failed",
			},
		})
	}
	_, _ = w.Write(body)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}
