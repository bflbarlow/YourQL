package models

import (
	"time"
)

type Login struct {
	ID            uint      `json:"id"`
	UserID        uint      `json:"user_id"` // defaults to 0 if not set
	Email         string    `json:"email"`
	Action        string    `json:"action"` // 'login' or 'logout'
	Success       bool      `json:"success"`
	Method        string    `json:"method"` // 'password', 'magic_code', or '' for logout
	IPAddress     string    `json:"ip_address"`
	UserAgent     string    `json:"user_agent"`
	FailureReason string    `json:"failure_reason"`
	CreatedAt     time.Time `json:"created_at"`
}