package models

import "time"

// Skill represents a user-defined markdown prompt fragment that can be
// injected into the system prompt to give the LLM additional context.
type Skill struct {
	ID              uint      `json:"id"`
	Name            string    `json:"name"`
	MarkdownContent string    `json:"markdown_content"`
	IsActive        bool      `json:"is_active"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}