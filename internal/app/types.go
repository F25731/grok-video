package app

import (
	"encoding/json"
	"strings"
)

type upstreamID string

func (id *upstreamID) UnmarshalJSON(data []byte) error {
	value := strings.TrimSpace(string(data))
	if value == "" || value == "null" {
		*id = ""
		return nil
	}
	if strings.HasPrefix(value, `"`) {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		*id = upstreamID(text)
		return nil
	}
	*id = upstreamID(value)
	return nil
}

func (id upstreamID) String() string {
	return string(id)
}

type videoRequest struct {
	Model       string
	Prompt      string
	Seconds     int
	N           int
	AspectRatio string
	Resolution  string
	ImageURLs   []string
}

type openAIVideo struct {
	ID          string         `json:"id"`
	TaskID      string         `json:"task_id,omitempty"`
	Object      string         `json:"object"`
	Model       string         `json:"model"`
	Status      string         `json:"status"`
	Progress    int            `json:"progress"`
	CreatedAt   int64          `json:"created_at"`
	CompletedAt int64          `json:"completed_at,omitempty"`
	Seconds     string         `json:"seconds,omitempty"`
	Error       *videoError    `json:"error,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

type videoError struct {
	Message string `json:"message"`
	Code    string `json:"code"`
}

type upstreamCreateResp struct {
	ID        upstreamID `json:"id"`
	TaskID    upstreamID `json:"task_id"`
	Object    string `json:"object"`
	Model     string `json:"model"`
	Status    string `json:"status"`
	Progress  any    `json:"progress"`
	CreatedAt int64  `json:"created_at"`
}

type upstreamPollResp struct {
	Code    string       `json:"code"`
	Message string       `json:"message"`
	Data    upstreamTask `json:"data"`
	Error   any          `json:"error"`
}

type upstreamTask struct {
	TaskID     upstreamID `json:"task_id"`
	ID         upstreamID `json:"id"`
	Status     string `json:"status"`
	Progress   string `json:"progress"`
	ResultURL  string `json:"result_url"`
	URL        string `json:"url"`
	VideoURL   string `json:"video_url"`
	Output     []string `json:"output"`
	Metadata   map[string]any `json:"metadata"`
	FailReason string `json:"fail_reason"`
	ErrorMessage string `json:"error_message"`
}
