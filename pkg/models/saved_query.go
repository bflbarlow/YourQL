package models

import (
	"time"
)

// SavedQuery represents a user's saved SQL query within a workspace.
type SavedQuery struct {
	ID             uint      `json:"id"`
	WorkspaceID    uint      `json:"workspace_id"`
	UserID         uint      `json:"user_id"`
	Name           string    `json:"name"`
	Description    *string   `json:"description,omitempty"`
	SQL            string    `json:"sql"`
	DBConnectionID *uint     `json:"db_connection_id,omitempty"`
	IsPublic       bool      `json:"is_public"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}
