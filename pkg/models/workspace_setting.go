package models

import (
	"time"
)

// WorkspaceSetting represents a key-value setting for a workspace.
type WorkspaceSetting struct {
	ID            uint      `json:"id"`
	WorkspaceID   uint      `json:"workspace_id"`
	Key           string    `json:"key"`
	Value         *string   `json:"value,omitempty"`
	Type          string    `json:"type"` // string, json, boolean
	UpdatedAt     time.Time `json:"updated_at"`
}
