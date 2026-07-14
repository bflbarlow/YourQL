package models

import (
	"time"
)

// Organization represents a managed organization.
// Only the data_app admin can create organizations.
// Organizations contain multiple workspaces and have their own membership system.
type Organization struct {
	ID        uint      `json:"id"`
	UUID      string    `json:"uuid"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	CreatedBy uint      `json:"created_by"` // FK → users (always data_app admin)
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// OrganizationMember represents a user's membership in an organization.
// Org roles are distinct from workspace roles:
//   - org roles (owner, admin, member) control org-level permissions
//   - workspace roles (owner, admin, member, viewer) control workspace-level permissions
type OrganizationMember struct {
	ID             uint      `json:"id"`
	OrganizationID uint      `json:"organization_id"`
	UserID         uint      `json:"user_id"`
	Role           string    `json:"role"` // owner, admin, member
	IsActive       bool      `json:"is_active"`
	JoinedAt       time.Time `json:"joined_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// IsValidOrgRole checks if a string is a valid organization role.
func IsValidOrgRole(role string) bool {
	switch role {
	case "owner", "admin", "member":
		return true
	default:
		return false
	}
}

// IsValidWorkspaceRole checks if a string is a valid workspace role.
func IsValidWorkspaceRole(role string) bool {
	switch role {
	case "owner", "admin", "member", "viewer":
		return true
	default:
		return false
	}
}
