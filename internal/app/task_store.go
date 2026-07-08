package app

import (
	"sort"
	"strings"
	"sync"
	"time"
)

type TaskRecord struct {
	ID          string         `json:"id"`
	Model       string         `json:"model"`
	Status      string         `json:"status"`
	Progress    int            `json:"progress"`
	ResultURL    string         `json:"result_url,omitempty"`
	Error       string         `json:"error,omitempty"`
	CreatedAt   int64          `json:"created_at"`
	UpdatedAt   int64          `json:"updated_at"`
	CompletedAt int64          `json:"completed_at,omitempty"`
	Polls       int            `json:"polls"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

type TaskStore struct {
	mu    sync.RWMutex
	items map[string]*TaskRecord
}

func NewTaskStore() *TaskStore {
	return &TaskStore{items: map[string]*TaskRecord{}}
}

func (s *TaskStore) Create(video openAIVideo) {
	if video.ID == "" {
		return
	}
	now := time.Now().Unix()
	createdAt := video.CreatedAt
	if createdAt <= 0 {
		createdAt = now
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[video.ID] = &TaskRecord{
		ID:        video.ID,
		Model:     video.Model,
		Status:    normalizeTaskStatus(video.Status),
		Progress:  video.Progress,
		CreatedAt: createdAt,
		UpdatedAt: now,
		Metadata:  video.Metadata,
	}
}

func (s *TaskStore) Update(video openAIVideo) {
	if video.ID == "" {
		return
	}
	now := time.Now().Unix()
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.items[video.ID]
	if item == nil {
		item = &TaskRecord{ID: video.ID, CreatedAt: firstPositive(video.CreatedAt, now)}
		s.items[video.ID] = item
	}
	item.Model = firstNonEmpty(video.Model, item.Model)
	item.Status = normalizeTaskStatus(video.Status)
	item.Progress = video.Progress
	item.UpdatedAt = now
	item.Polls++
	item.Metadata = video.Metadata
	if value, ok := video.Metadata["video_url"].(string); ok {
		item.ResultURL = value
	} else if value, ok := video.Metadata["url"].(string); ok {
		item.ResultURL = value
	}
	if video.Error != nil {
		item.Error = video.Error.Message
	}
	if video.CompletedAt > 0 {
		item.CompletedAt = video.CompletedAt
	}
}

func (s *TaskStore) SetContentFetched(taskID string) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if item := s.items[taskID]; item != nil {
		item.UpdatedAt = time.Now().Unix()
	}
}

func (s *TaskStore) ModelFor(taskID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if item := s.items[taskID]; item != nil && strings.TrimSpace(item.Model) != "" {
		return item.Model
	}
	return "grok-video"
}

func (s *TaskStore) Stats() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	stats := map[string]int{
		"total":       len(s.items),
		"queued":      0,
		"in_progress": 0,
		"completed":   0,
		"failed":      0,
	}
	for _, item := range s.items {
		switch normalizeTaskStatus(item.Status) {
		case "completed":
			stats["completed"]++
		case "failed":
			stats["failed"]++
		case "in_progress":
			stats["in_progress"]++
		default:
			stats["queued"]++
		}
	}
	return map[string]any{
		"total":       stats["total"],
		"queued":      stats["queued"],
		"in_progress": stats["in_progress"],
		"completed":   stats["completed"],
		"failed":      stats["failed"],
	}
}

func (s *TaskStore) Recent(limit int) []TaskRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 {
		limit = 20
	}
	items := make([]TaskRecord, 0, len(s.items))
	for _, item := range s.items {
		items = append(items, *item)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].UpdatedAt > items[j].UpdatedAt
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items
}

func normalizeTaskStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "success", "succeeded", "done", "finished":
		return "completed"
	case "failed", "failure", "timeout", "canceled", "cancelled":
		return "failed"
	case "in_progress", "running", "processing":
		return "in_progress"
	default:
		return "queued"
	}
}

func firstPositive(values ...int64) int64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}
