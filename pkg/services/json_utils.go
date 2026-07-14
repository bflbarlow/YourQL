package services

import (
	"encoding/json"
)

// isValidJSON checks if a string is valid JSON.
func isValidJSON(s string) bool {
	var js interface{}
	return json.Unmarshal([]byte(s), &js) == nil
}
