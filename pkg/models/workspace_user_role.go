package models

import (
	"time"
)

// WorkspaceUserRole represents a role assignment for a user within a workspace.
type WorkspaceUserRole struct {
	ID              uint      `json:"id"`
	WorkspaceUserID uint      `json:"workspace_user_id"`
	WorkspaceRoleID uint      `json:"workspace_role_id"`
	AssignedBy      *uint     `json:"assigned_by,omitempty"`
	AssignedAt      time.Time `json:"assigned_at"`
}
