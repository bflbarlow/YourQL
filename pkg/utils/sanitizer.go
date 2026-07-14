package utils

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// SanitizeString removes potentially dangerous characters and trims whitespace
func SanitizeString(input string) string {
	if input == "" {
		return ""
	}

	// Trim leading and trailing whitespace
	input = strings.TrimSpace(input)

	// Remove null bytes
	input = strings.ReplaceAll(input, "\x00", "")

	// Remove control characters (except tab, newline, carriage return)
	input = strings.Map(func(r rune) rune {
		if r < 32 && r != '\t' && r != '\n' && r != '\r' {
			return -1
		}
		return r
	}, input)

	// Limit length to prevent buffer overflow attacks
	if len(input) > 10000 {
		input = input[:10000]
	}

	return input
}

// SanitizeEmail validates and sanitizes an email address
func SanitizeEmail(input string) string {
	input = SanitizeString(input)
	input = strings.ToLower(input)

	// Basic email format validation
	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	if !emailRegex.MatchString(input) {
		return ""
	}

	// Limit email length
	if len(input) > 254 {
		input = input[:254]
	}

	return input
}

// SanitizePassword validates and sanitizes a password
func SanitizePassword(input string) string {
	// Don't trim passwords (whitespace might be intentional)
	// But remove null bytes
	input = strings.ReplaceAll(input, "\x00", "")

	// Remove control characters
	input = strings.Map(func(r rune) rune {
		if r < 32 {
			return -1
		}
		return r
	}, input)

	// Limit password length
	if len(input) > 128 {
		input = input[:128]
	}

	return input
}

// SanitizeUsername sanitizes a username or display name
func SanitizeUsername(input string) string {
	input = SanitizeString(input)

	// Only allow alphanumeric, spaces, hyphens, and underscores
	usernameRegex := regexp.MustCompile(`[^a-zA-Z0-9\s\-_]`)
	input = usernameRegex.ReplaceAllString(input, "")

	// Limit length
	if len(input) > 50 {
		input = input[:50]
	}

	return input
}

// SanitizeToken sanitizes a token (alphanumeric only)
func SanitizeToken(input string) string {
	input = strings.TrimSpace(input)

	// Only allow alphanumeric characters
	tokenRegex := regexp.MustCompile(`[^a-zA-Z0-9]`)
	input = tokenRegex.ReplaceAllString(input, "")

	// Limit length based on token type
	if len(input) > 256 {
		input = input[:256]
	}

	return input
}

// SanitizeCode sanitizes a 6-digit login code
func SanitizeCode(input string) string {
	input = strings.TrimSpace(input)

	// Only allow digits
	codeRegex := regexp.MustCompile(`[^0-9]`)
	input = codeRegex.ReplaceAllString(input, "")

	// Must be exactly 6 digits
	if len(input) != 6 {
		return ""
	}

	return input
}

// SanitizeURL sanitizes a URL
func SanitizeURL(input string) string {
	input = SanitizeString(input)

	// Basic URL validation
	urlRegex := regexp.MustCompile(`^https?://[^\s]+$`)
	if !urlRegex.MatchString(input) {
		return ""
	}

	// Limit URL length
	if len(input) > 2048 {
		input = input[:2048]
	}

	return input
}

// SanitizeIP sanitizes an IP address
func SanitizeIP(input string) string {
	input = strings.TrimSpace(input)

	// IPv4 validation
	ipv4Regex := regexp.MustCompile(`^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}$`)
	if ipv4Regex.MatchString(input) {
		parts := strings.Split(input, ".")
		for _, part := range parts {
			if num, err := strconv.Atoi(part); err != nil || num > 255 {
				return ""
			}
		}
		return input
	}

	// IPv6 validation (simplified)
	ipv6Regex := regexp.MustCompile(`^[0-9a-fA-F:]+$`)
	if ipv6Regex.MatchString(input) && strings.Count(input, ":") >= 2 {
		if len(input) <= 45 {
			return input
		}
	}

	return ""
}

// SanitizeUserAgent sanitizes a User-Agent string
func SanitizeUserAgent(input string) string {
	input = SanitizeString(input)

	// Remove any null bytes or control characters
	input = strings.Map(func(r rune) rune {
		if r < 32 || r > 126 {
			return -1
		}
		return r
	}, input)

	// Limit User-Agent length
	if len(input) > 512 {
		input = input[:512]
	}

	return input
}

// SanitizeRedirectURL validates a redirect URL to prevent open redirects
func SanitizeRedirectURL(input string, allowedDomains []string) string {
	input = SanitizeString(input)

	// If it's a relative path, allow it
	if strings.HasPrefix(input, "/") && !strings.HasPrefix(input, "//") {
		if len(input) <= 2048 {
			return input
		}
		return ""
	}

	// If it's an absolute URL, check if it's from an allowed domain
	urlRegex := regexp.MustCompile(`^https?://([^/]+)`)
	matches := urlRegex.FindStringSubmatch(input)
	if len(matches) > 1 {
		domain := strings.ToLower(matches[1])
		// Remove port if present
		if idx := strings.Index(domain, ":"); idx != -1 {
			domain = domain[:idx]
		}

		for _, allowed := range allowedDomains {
			if domain == strings.ToLower(allowed) {
				if len(input) <= 2048 {
					return input
				}
				break
			}
		}
	}

	// Default to safe redirect
	return "/"
}

// IsSafeString checks if a string contains only safe characters
func IsSafeString(input string, minLen, maxLen int) bool {
	if len(input) < minLen || len(input) > maxLen {
		return false
	}

	for _, r := range input {
		if !unicode.IsPrint(r) && r != '\t' && r != '\n' && r != '\r' {
			return false
		}
	}

	return utf8.ValidString(input)
}

// AtoiSafe safely converts a string to an integer with bounds checking
func AtoiSafe(input string, min, max int) (int, bool) {
	input = strings.TrimSpace(input)

	// Check if string contains only digits
	for _, r := range input {
		if r < '0' || r > '9' {
			return 0, false
		}
	}

	if len(input) == 0 {
		return 0, false
	}

	// Convert to int
	result := 0
	for _, c := range input {
		result = result*10 + int(c-'0')
	}

	if result < min || result > max {
		return 0, false
	}

	return result, true
}

// SanitizeHTML removes HTML tags from a string (prevents XSS)
func SanitizeHTML(input string) string {
	// Remove HTML tags
	htmlRegex := regexp.MustCompile(`<[^>]*>`)
	input = htmlRegex.ReplaceAllString(input, "")

	// Remove JavaScript event handlers
	eventRegex := regexp.MustCompile(`\bon\w+\s*=\s*["'][^"']*["']`)
	input = eventRegex.ReplaceAllString(input, "")

	// Remove javascript: protocol
	jsProtocolRegex := regexp.MustCompile(`javascript\s*:`)
	input = jsProtocolRegex.ReplaceAllString(input, "")

	// Remove data: protocol (can be used for XSS)
	dataProtocolRegex := regexp.MustCompile(`data\s*:\s*text/html`)
	input = dataProtocolRegex.ReplaceAllString(input, "")

	return SanitizeString(input)
}

// SanitizeJSONInput sanitizes a JSON input string
func SanitizeJSONInput(input string) string {
	// Limit size to prevent DoS
	if len(input) > 1024*1024 { // 1MB limit
		return input[:1024*1024]
	}

	// Remove null bytes
	input = strings.ReplaceAll(input, "\x00", "")

	return input
}