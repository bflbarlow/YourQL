package models

import (
	"time"
)

// Workspace represents a multi-tenant workspace.
// All configuration (LLM providers, DB connections, users, etc.) is scoped to a Workspace.
// organization_id = NULL means personal workspace (org-less).
// is_personal = 1 is an explicit marker for personal workspaces.
type Workspace struct {
	ID                    uint        `json:"id"`
	UUID                  string      `json:"uuid"`
	Name                  string      `json:"name"`
	Slug                  string      `json:"slug"`
	Description           string      `json:"description,omitempty"`
	OwnerID               uint        `json:"owner_id"`
	OrganizationID        *uint       `json:"organization_id,omitempty"` // NULL = personal workspace
	IsPersonal            bool        `json:"is_personal"`               // explicit marker for personal workspaces
	DefaultLLMProvider    string      `json:"default_llm_provider,omitempty"`
	DefaultDBConnection   string      `json:"default_db_connection,omitempty"`
	Settings              *string     `json:"settings,omitempty"` // JSON string
	IsActive              bool        `json:"is_active"`
	CreatedAt             time.Time   `json:"created_at"`
	UpdatedAt             time.Time   `json:"updated_at"`
}
