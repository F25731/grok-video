package app

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
)

type Server struct {
	cfg    Config
	client *UpstreamClient
	tasks  sync.Map
}

func NewServer(cfg Config) http.Handler {
	s := &Server{cfg: cfg, client: NewUpstreamClient(cfg)}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.health)
	mux.HandleFunc("/v1/models", s.withAuth(s.models))
	mux.HandleFunc("/v1/videos", s.withAuth(s.createVideo))
	mux.HandleFunc("/v1/videos/", s.withAuth(s.videoByID))
	mux.HandleFunc("/v1/video/generations", s.withAuth(s.createVideo))
	mux.HandleFunc("/v1/video/generations/", s.withAuth(s.videoGenerationByID))
	return withCORS(mux)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.WrapperAPIKey != "" {
			token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if strings.TrimSpace(token) != s.cfg.WrapperAPIKey {
				writeError(w, http.StatusUnauthorized, "invalid wrapper api key")
				return
			}
		}
		next(w, r)
	}
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{
			"message": message,
			"type":    "invalid_request_error",
			"code":    "upstream_failed",
		},
	})
}
