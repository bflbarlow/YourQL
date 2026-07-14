package models

import (
	"time"
)

// WorkspaceUser represents a user's membership in a workspace.
type WorkspaceUser struct {
	ID          uint      `json:"id"`
	WorkspaceID uint      `json:"workspace_id"`
	UserID      uint      `json:"user_id"`
	Role        string    `json:"role"` // owner, admin, member, viewer
	IsActive    bool      `json:"is_active"`
	JoinedAt    time.Time `json:"joined_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
