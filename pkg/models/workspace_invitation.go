package models

import (
	"time"
)

// WorkspaceInvitation represents an invitation to join a workspace.
type WorkspaceInvitation struct {
	ID          uint        `json:"id"`
	WorkspaceID uint        `json:"workspace_id"`
	Email       string      `json:"email"`
	Role        string      `json:"role"`
	InvitedBy   uint        `json:"invited_by"`
	Token       string      `json:"token"`
	ExpiresAt   time.Time   `json:"expires_at"`
	AcceptedAt  *time.Time  `json:"accepted_at,omitempty"`
	CreatedAt   time.Time   `json:"created_at"`
}
