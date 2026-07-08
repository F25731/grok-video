package app

import "net/http"

type modelSpec struct {
	ID           string
	MaxImages    int
	RequireImage bool
	TextToVideo  bool
	MaxSeconds   int
	MultiMaxSec  int
	Ratios       map[string]bool
}

func modelSpecs() map[string]modelSpec {
	return map[string]modelSpec{
		"grok-image-video": {
			ID:          "grok-image-video",
			MaxImages:   7,
			TextToVideo: true,
			MaxSeconds:  15,
			MultiMaxSec: 10,
			Ratios:      ratios("1:1", "16:9", "9:16", "4:3", "3:4", "3:2", "2:3"),
		},
		"grok-video-1.5": {
			ID:           "grok-video-1.5",
			MaxImages:    1,
			RequireImage: true,
			MaxSeconds:   15,
			Ratios:       ratios("16:9", "9:16"),
		},
	}
}

func (s *Server) models(w http.ResponseWriter, r *http.Request) {
	data := make([]map[string]any, 0, len(modelSpecs()))
	for id := range modelSpecs() {
		data = append(data, map[string]any{
			"id":                       id,
			"object":                   "model",
			"created":                  0,
			"owned_by":                 "grok-video-wrapper",
			"supported_endpoint_types": []string{"images", "videos"},
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": data})
}

func ratios(values ...string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}
