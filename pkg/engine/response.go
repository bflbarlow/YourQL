package engine

import (
	"encoding/json"
	"regexp"
	"strings"
)

// TruncateString truncates a string to maxLen with ellipsis.
func TruncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// StripHTMLTags removes HTML tags from a string.
func StripHTMLTags(s string) string {
	re := regexp.MustCompile(`<[^>]*>`)
	return re.ReplaceAllString(s, "")
}

// StringPtr returns a pointer to s.
func StringPtr(s string) *string { return &s }

// SanitizeSQLError strips embedded DSN credentials from database driver
// error messages. Per AGENT_READ_FIRST.md §3.5/§4.0, credential logging is
// never acceptable.
func SanitizeSQLError(msg string) string {
	if msg == "" {
		return ""
	}
	re := regexp.MustCompile(`(://[^:@]+):[^@]+@`)
	msg = re.ReplaceAllString(msg, "$1:***@")
	re2 := regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9]*):[^@\s]+@`)
	msg = re2.ReplaceAllString(msg, "$1:***@")
	return msg
}

// FormatUserError maps an error to a user-friendly message.
// Raw error details are preserved elsewhere (for debugging) — this function
// produces only the text shown in the chat bubble.
func FormatUserError(err error) string {
	if err == nil {
		return "I encountered an unexpected issue. Please try again."
	}
	msg := err.Error()
	lower := strings.ToLower(msg)

	if strings.Contains(lower, "api key") || strings.Contains(lower, "unauthorized") ||
		strings.Contains(lower, "forbidden") || strings.Contains(lower, "authentication") ||
		strings.Contains(lower, "access denied") || strings.Contains(lower, "permission denied") {
		return "Authentication failed. Please check your API key and connection credentials in Settings."
	}
	if strings.Contains(lower, "rate limit") || strings.Contains(lower, "too many requests") {
		return "The AI model is receiving too many requests right now. Please wait a moment and try again."
	}
	if strings.Contains(lower, "no database connection") || strings.Contains(lower, "cannot execute sql without") {
		return "No database connection configured. Please add a data source in Settings."
	}
	if strings.Contains(lower, "dial") || strings.Contains(lower, "connection refused") ||
		strings.Contains(lower, "i/o timeout") || strings.Contains(lower, "connection reset") ||
		strings.Contains(lower, "no such host") || strings.Contains(lower, "tls") {
		return "I'm having trouble connecting to the database. Please check your connection settings and try again."
	}
	if strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline") {
		return "The request is taking longer than expected. Please try again."
	}
	if strings.Contains(lower, "syntax error") || strings.Contains(lower, "unknown column") ||
		strings.Contains(lower, "unknown table") || strings.Contains(lower, "doesn't exist") ||
		strings.Contains(lower, "does not exist") || strings.Contains(lower, "you have an error in your") {
		return "I had trouble understanding your question. Could you try rephrasing it?"
	}
	if strings.Contains(lower, "twice in a row") || strings.Contains(lower, "loop detected") ||
		strings.Contains(lower, "query loop") {
		return "I'm having trouble generating a new query. Could you try rephrasing your question?"
	}
	if strings.Contains(lower, "failed to fetch database schema") || strings.Contains(lower, "unable to load database schema") {
		return "Unable to load your database schema. Please check your connection settings."
	}
	return "I encountered an issue processing your request. Please try again or rephrase your question."
}

// BuildErrorMetadata creates metadata JSON for error messages with the raw
// error string preserved for the tech-details panel.
func BuildErrorMetadata(rawError string) *string {
	metadataJSON, _ := json.Marshal(map[string]interface{}{
		"content_type": "html",
		"raw_error":    SanitizeSQLError(rawError),
		"is_error":     true,
	})
	metadata := string(metadataJSON)
	return &metadata
}

// ClassifyErrorCategory maps an error to a stable error_category value.
func ClassifyErrorCategory(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())

	if strings.Contains(msg, "api key") || strings.Contains(msg, "unauthorized") ||
		strings.Contains(msg, "forbidden") || strings.Contains(msg, "authentication") ||
		strings.Contains(msg, "access denied") || strings.Contains(msg, "permission denied") {
		return "auth_failure"
	}
	if strings.Contains(msg, "rate limit") || strings.Contains(msg, "too many requests") {
		return "rate_limit"
	}
	if strings.Contains(msg, "dial") || strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "i/o timeout") || strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "no such host") || strings.Contains(msg, "tls") ||
		strings.Contains(msg, "handshake") {
		return "transient_network"
	}
	if strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline") {
		return "llm_timeout"
	}
	if strings.Contains(msg, "syntax error") || strings.Contains(msg, "unknown column") ||
		strings.Contains(msg, "unknown table") || strings.Contains(msg, "doesn't exist") ||
		strings.Contains(msg, "does not exist") || strings.Contains(msg, "you have an error in your") {
		return "bad_query"
	}
	if strings.Contains(msg, "twice in a row") || strings.Contains(msg, "loop detected") ||
		strings.Contains(msg, "query loop") {
		return "query_loop"
	}
	if strings.Contains(msg, "failed to fetch database schema") || strings.Contains(msg, "unable to load database schema") {
		return "schema_failure"
	}
	if strings.Contains(msg, "no database connection") || strings.Contains(msg, "cannot execute sql without") {
		return "no_db_connection"
	}
	if strings.Contains(msg, "cancelled") {
		return "user_cancelled"
	}
	return "internal_error"
}

// FormatSkillsContext formats active skills into a prompt-friendly block.
func FormatSkillsContext(skillsContent string) string {
	if skillsContent == "" {
		return ""
	}
	return "\n## Additional Context (from Skills)\n" + skillsContent + "\n"
}

// IsErrorMessage checks whether a message's metadata flags it as an error
// response that should be excluded from the LLM context window.
func IsErrorMessage(metadata *string) bool {
	if metadata == nil {
		return false
	}
	var meta map[string]interface{}
	if err := json.Unmarshal([]byte(*metadata), &meta); err != nil {
		return false
	}
	if isErr, ok := meta["is_error"]; ok {
		if b, ok := isErr.(bool); ok {
			return b
		}
	}
	return false
}