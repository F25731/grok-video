package app

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type TaskRecord struct {
	ID          string         `json:"id"`
	Model       string         `json:"model"`
	Status      string         `json:"status"`
	Progress    int            `json:"progress"`
	Prompt      string         `json:"prompt,omitempty"`
	AspectRatio string         `json:"aspect_ratio,omitempty"`
	Resolution  string         `json:"resolution,omitempty"`
	Seconds     int            `json:"seconds,omitempty"`
	ImageURLs   []string       `json:"image_urls,omitempty"`
	ResultURL   string         `json:"result_url,omitempty"`
	Error       string         `json:"error,omitempty"`
	CreatedAt   int64          `json:"created_at"`
	UpdatedAt   int64          `json:"updated_at"`
	CompletedAt int64          `json:"completed_at,omitempty"`
	Polls       int            `json:"polls"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

type TaskStore struct {
	mu          sync.RWMutex
	items       map[string]*TaskRecord
	path        string
	subscribers map[chan []TaskRecord]struct{}
}

func NewTaskStore(dataDir string) *TaskStore {
	store := &TaskStore{
		items:       map[string]*TaskRecord{},
		path:        filepath.Join(dataDir, "tasks.jsonl"),
		subscribers: map[chan []TaskRecord]struct{}{},
	}
	_ = store.load()
	return store
}

func (s *TaskStore) Create(video openAIVideo, req videoRequest) {
	if video.ID == "" {
		return
	}
	now := time.Now().Unix()
	createdAt := video.CreatedAt
	if createdAt <= 0 {
		createdAt = now
	}
	record := &TaskRecord{
		ID:          video.ID,
		Model:       firstNonEmpty(video.Model, req.Model),
		Status:      normalizeTaskStatus(video.Status),
		Progress:    video.Progress,
		Prompt:      req.Prompt,
		AspectRatio: req.AspectRatio,
		Resolution:  req.Resolution,
		Seconds:     req.Seconds,
		ImageURLs:   append([]string(nil), req.ImageURLs...),
		CreatedAt:   createdAt,
		UpdatedAt:   now,
		Metadata:    video.Metadata,
	}
	s.upsert(record, true)
}

func (s *TaskStore) Update(video openAIVideo) {
	if video.ID == "" {
		return
	}
	now := time.Now().Unix()
	s.mu.Lock()
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
	if video.Seconds != "" && item.Seconds <= 0 {
		item.Seconds = atoi(video.Seconds)
	}
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
	snapshot := *item
	s.mu.Unlock()
	s.persistAndBroadcast(snapshot)
}

func (s *TaskStore) SetContentFetched(taskID string) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return
	}
	s.mu.Lock()
	if item := s.items[taskID]; item != nil {
		item.UpdatedAt = time.Now().Unix()
		snapshot := *item
		s.mu.Unlock()
		s.persistAndBroadcast(snapshot)
		return
	}
	s.mu.Unlock()
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
	return s.recentLocked(limit)
}

func (s *TaskStore) Subscribe(limit int) (chan []TaskRecord, func()) {
	ch := make(chan []TaskRecord, 4)
	s.mu.Lock()
	s.subscribers[ch] = struct{}{}
	ch <- s.recentLocked(limit)
	s.mu.Unlock()
	return ch, func() {
		s.mu.Lock()
		delete(s.subscribers, ch)
		s.mu.Unlock()
	}
}

func (s *TaskStore) upsert(record *TaskRecord, overwrite bool) {
	s.mu.Lock()
	if current := s.items[record.ID]; current != nil && !overwrite {
		mergeTaskRecord(current, record)
		record = current
	} else if current := s.items[record.ID]; current != nil {
		mergeTaskRecord(record, current)
		s.items[record.ID] = record
	} else {
		s.items[record.ID] = record
	}
	snapshot := *record
	s.mu.Unlock()
	s.persistAndBroadcast(snapshot)
}

func (s *TaskStore) persistAndBroadcast(record TaskRecord) {
	_ = s.append(record)
	s.broadcast()
}

func (s *TaskStore) broadcast() {
	s.mu.RLock()
	snapshot := s.recentLocked(80)
	subscribers := make([]chan []TaskRecord, 0, len(s.subscribers))
	for ch := range s.subscribers {
		subscribers = append(subscribers, ch)
	}
	s.mu.RUnlock()
	for _, ch := range subscribers {
		select {
		case ch <- snapshot:
		default:
		}
	}
}

func (s *TaskStore) recentLocked(limit int) []TaskRecord {
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

func (s *TaskStore) load() error {
	file, err := os.Open(s.path)
	if err != nil {
		return nil
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	buffer := make([]byte, 64*1024)
	scanner.Buffer(buffer, 2*1024*1024)
	for scanner.Scan() {
		var record TaskRecord
		if json.Unmarshal(scanner.Bytes(), &record) == nil && record.ID != "" {
			current := s.items[record.ID]
			if current == nil || record.UpdatedAt >= current.UpdatedAt {
				copy := record
				s.items[record.ID] = &copy
			}
		}
	}
	return scanner.Err()
}

func (s *TaskStore) append(record TaskRecord) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	payload, err := json.Marshal(record)
	if err != nil {
		return err
	}
	_, err = file.Write(append(payload, '\n'))
	return err
}

func mergeTaskRecord(dst, src *TaskRecord) {
	dst.Model = firstNonEmpty(dst.Model, src.Model)
	dst.Prompt = firstNonEmpty(dst.Prompt, src.Prompt)
	dst.AspectRatio = firstNonEmpty(dst.AspectRatio, src.AspectRatio)
	dst.Resolution = firstNonEmpty(dst.Resolution, src.Resolution)
	if dst.Seconds <= 0 {
		dst.Seconds = src.Seconds
	}
	if len(dst.ImageURLs) == 0 {
		dst.ImageURLs = append([]string(nil), src.ImageURLs...)
	}
	if dst.CreatedAt <= 0 {
		dst.CreatedAt = src.CreatedAt
	}
}

func normalizeTaskStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "success", "succeeded", "done", "finished":
		return "completed"
	case "failed", "failure", "timeout", "canceled", "cancelled", "error":
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

func atoi(value string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(value))
	return n
}
