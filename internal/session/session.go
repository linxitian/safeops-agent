package session

import "time"

type MessageRole string

const (
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
	RoleSystem    MessageRole = "system"
)

type Message struct {
	ID        string      `json:"message_id"`
	Role      MessageRole `json:"role"`
	Content   string      `json:"content"`
	TaskID    string      `json:"task_id,omitempty"`
	CreatedAt time.Time   `json:"created_at"`
}

type Session struct {
	ID                string            `json:"session_id"`
	Name              string            `json:"name"`
	Archived          bool              `json:"archived"`
	Messages          []Message         `json:"messages"`
	Summary           string            `json:"summary,omitempty"`
	PinnedContext     map[string]string `json:"pinned_context,omitempty"`
	SelectedResources []string          `json:"selected_resources,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}
