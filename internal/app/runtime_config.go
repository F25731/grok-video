package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type RuntimeConfig struct {
	UpstreamAPIKey string `json:"upstream_api_key"`
}

type RuntimeConfigStore struct {
	path string
	mu   sync.RWMutex
	data RuntimeConfig
}

func NewRuntimeConfigStore(cfg Config) (*RuntimeConfigStore, error) {
	path := cfg.RuntimeConfig
	if path == "" {
		path = filepath.Join(cfg.DataDir, "config.json")
	}
	store := &RuntimeConfigStore{path: path}
	if err := store.load(cfg.UpstreamAPIKey); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *RuntimeConfigStore) load(seedKey string) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	data, err := os.ReadFile(s.path)
	if err == nil {
		var cfg RuntimeConfig
		if json.Unmarshal(data, &cfg) == nil {
			cfg.UpstreamAPIKey = strings.TrimSpace(cfg.UpstreamAPIKey)
			s.data = cfg
			return nil
		}
	}
	s.data = RuntimeConfig{UpstreamAPIKey: strings.TrimSpace(seedKey)}
	if s.data.UpstreamAPIKey != "" {
		return s.Save(s.data)
	}
	return nil
}

func (s *RuntimeConfigStore) Get() RuntimeConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data
}

func (s *RuntimeConfigStore) Save(cfg RuntimeConfig) error {
	cfg.UpstreamAPIKey = strings.TrimSpace(cfg.UpstreamAPIKey)
	payload, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(s.path, payload, 0600); err != nil {
		return err
	}
	s.mu.Lock()
	s.data = cfg
	s.mu.Unlock()
	return nil
}
