package models

import (
	"time"
)

type User struct {
	ID                   uint      `json:"id"`
	Email                string    `json:"email"`
	Password             string    `json:"-"` // Never include in JSON responses
	FirstName            string    `json:"first_name,omitempty"`
	LastName             string    `json:"last_name,omitempty"`
	Name                 string    `json:"name,omitempty"`
	IsVerified           bool      `json:"is_verified"`
	IsAdmin              bool      `json:"is_admin"`
	ConfirmationToken    string    `json:"-"` // Never include in JSON responses
	MagicCode            string    `json:"-"` // Magic login code - never expose
	MagicCodeExpires     time.Time `json:"-"` // Magic code expiration - never expose
	PasswordResetToken   string    `json:"-"` // Password reset token - never expose
	PasswordResetExpires time.Time `json:"-"` // Password reset token expiration - never expose
	FailedLoginAttempts  int       `json:"-"` // Failed login attempts counter
	LockedUntil          *time.Time `json:"-"` // Account lockout expiration - never expose
	LastFailedLoginAt    *time.Time `json:"-"` // Last failed login timestamp - never expose
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}