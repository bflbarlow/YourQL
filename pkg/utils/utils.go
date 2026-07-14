package utils

import (
	"fmt"
	"math/rand"
	"regexp"
	"time"
	"unicode"

	"YourQL/pkg/configuration"
)

// ValidatePasswordStrength validates a password against configured requirements.
// Returns an empty string if valid, or an error message explaining the validation failure.
func ValidatePasswordStrength(password string) string {
	config := configuration.Config.Password

	if len(password) < config.MinLength {
		return "Password must be at least " + fmt.Sprint(config.MinLength) + " characters long"
	}

	hasUpper := false
	hasLower := false
	hasDigit := false
	hasSpecial := false

	for _, char := range password {
		switch {
		case unicode.IsUpper(char):
			hasUpper = true
		case unicode.IsLower(char):
			hasLower = true
		case unicode.IsDigit(char):
			hasDigit = true
		case unicode.IsPunct(char) || unicode.IsSymbol(char):
			hasSpecial = true
		}
	}

	if config.RequireUppercase && !hasUpper {
		return "Password must contain at least one uppercase letter"
	}
	if config.RequireLowercase && !hasLower {
		return "Password must contain at least one lowercase letter"
	}
	if config.RequireDigit && !hasDigit {
		return "Password must contain at least one digit"
	}
	if config.RequireSpecial && !hasSpecial {
		return "Password must contain at least one special character"
	}

	return ""
}

// ValidateEmail validates email format using regex
func ValidateEmail(email string) bool {
	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
	return emailRegex.MatchString(email)
}

// GenerateTicketCode generates a unique ticket code
func GenerateTicketCode() string {
	timestamp := time.Now().UnixNano()
	// Use a simple hash with timestamp and random number
	rand.Seed(timestamp)
	random := rand.Intn(10000)
	code := fmt.Sprintf("TKT-%d-%04d", timestamp % 1000000, random)
	return code
}