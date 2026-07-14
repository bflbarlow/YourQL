package models

import (
	"time"
)

// WorkspaceRole represents a custom role definition within a workspace.
type WorkspaceRole struct {
	ID          uint      `json:"id"`
	WorkspaceID uint      `json:"workspace_id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Permissions *string   `json:"permissions,omitempty"` // JSON string
	IsSystem    bool      `json:"is_system"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
