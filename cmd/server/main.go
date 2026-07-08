package main

import (
	"log"
	"net/http"

	"grok-video-wrapper/internal/app"
)

func main() {
	cfg := app.LoadConfig()
	server := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: app.NewServer(cfg),
	}
	log.Printf("grok video wrapper listening on :%s", cfg.Port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server failed: %v", err)
	}
}
