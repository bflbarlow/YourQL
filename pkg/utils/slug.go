package utils

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"YourQL/pkg/models"
)

// GenerateSlug creates a URL-friendly slug from a name.
func GenerateSlug(name string) string {
	slug := strings.ToLower(name)

	var builder strings.Builder
	for _, r := range slug {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' {
			builder.WriteRune(r)
		} else {
			builder.WriteRune('-')
		}
	}
	slug = builder.String()
	slug = strings.ReplaceAll(slug, "--", "-")
	slug = strings.Trim(slug, "-")
	if len(slug) > 100 {
		slug = slug[:100]
		slug = strings.Trim(slug, "-")
	}
	return slug
}

// EnsureUniqueSlug generates a slug and appends a random suffix if the slug already exists.
// When orgID is nil, checks global slug uniqueness (for personal workspaces).
// When orgID is non-nil, checks uniqueness within that org (compound unique).
func EnsureUniqueSlug(name string, orgID *uint) (string, error) {
	baseSlug := GenerateSlug(name)
	slug := baseSlug

	for i := 0; i < 10; i++ {
		exists, err := WorkspaceSlugExists(slug, orgID)
		if err != nil {
			return "", err
		}
		if !exists {
			return slug, nil
		}
		suffix := make([]byte, 3)
		if _, err := rand.Read(suffix); err != nil {
			return "", err
		}
		slug = fmt.Sprintf("%s-%s", baseSlug, hex.EncodeToString(suffix)[:4])
	}

	return "", errors.New("failed to generate unique slug after 10 attempts")
}

// WorkspaceSlugExists checks if a workspace slug already exists.
// When orgID is nil, checks global slug uniqueness (for personal workspaces).
// When orgID is non-nil, checks uniqueness within that org (compound unique).
func WorkspaceSlugExists(slug string, orgID *uint) (bool, error) {
	var count int
	var err error
	if orgID != nil {
		err = models.DB.QueryRow(
			"SELECT COUNT(*) FROM workspaces WHERE slug = ? AND organization_id = ?", slug, *orgID,
		).Scan(&count)
	} else {
		err = models.DB.QueryRow(
			"SELECT COUNT(*) FROM workspaces WHERE slug = ?", slug,
		).Scan(&count)
	}
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// EnsureUniqueOrgSlug generates a unique slug for organizations.
// Uses org-{sanitized-name} prefix convention.
func EnsureUniqueOrgSlug(name string) (string, error) {
	return EnsureUniqueSlug("org-"+name, nil)
}

// EnsureUniquePersonalSlug generates a unique slug for personal workspaces.
// Uses me-{sanitized-email} prefix convention.
func EnsureUniquePersonalSlug(email string) (string, error) {
	return EnsureUniqueSlug("me-"+email, nil)
}
