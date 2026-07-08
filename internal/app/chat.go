package app

import (
	"encoding/json"
	"net/http"
	"time"
)

type chatCompletionRequest struct {
	Model string `json:"model"`
}

func (s *Server) chatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	req := chatCompletionRequest{}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req)
	if req.Model == "" {
		req.Model = "grok-video-wrapper"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":      "chatcmpl-wrapper-test",
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   req.Model,
		"choices": []map[string]any{
			{
				"index": 0,
				"message": map[string]string{
					"role":    "assistant",
					"content": "ok",
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]int{
			"prompt_tokens":     1,
			"completion_tokens": 1,
			"total_tokens":      2,
		},
	})
}
