package services

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"YourQL/pkg/models"
	"YourQL/pkg/utils"
)

// ─── Organization CRUD ─────────────────────────────────────────────

// CreateOrganization creates a new organization and adds the first admin.
// Only data_app admin can call this. Returns the created organization.
func CreateOrganization(name, slug string, createdBy uint, firstAdminEmail string, firstAdminRole string) (*models.Organization, error) {
	if slug == "" {
		slug = utils.GenerateSlug(name)
	}

	// Ensure unique slug
	slug, err := utils.EnsureUniqueOrgSlug(name)
	if err != nil {
		return nil, fmt.Errorf("failed to generate unique org slug: %w", err)
	}

	// Validate org role
	if !models.IsValidOrgRole(firstAdminRole) {
		return nil, fmt.Errorf("invalid org role: %s (must be owner, admin, or member)", firstAdminRole)
	}

	uuid, err := GenerateUUID()
	if err != nil {
		return nil, fmt.Errorf("failed to generate UUID: %w", err)
	}

	now := time.Now().UTC()

	tx, err := models.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	// Create organization
	result, err := tx.Exec(
		"INSERT INTO organizations (uuid, name, slug, created_by, is_active, created_at, updated_at) VALUES (?, ?, ?, ?, 1, ?, ?)",
		uuid, name, slug, createdBy, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create organization: %w", err)
	}

	orgID, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get organization ID: %w", err)
	}

	// Find or create the first admin user
	var adminUserID uint

	// Check if user exists
	var existingEmail sql.NullString
	err = tx.QueryRow("SELECT email FROM users WHERE email = ? LIMIT 1", firstAdminEmail).Scan(&existingEmail)
	if err == sql.ErrNoRows {
		// User doesn't exist — create account with random password
		randomPassword, pwdErr := GenerateMagicCode() // 6-digit temp password
		if pwdErr != nil {
			return nil, fmt.Errorf("failed to generate temp password: %w", pwdErr)
		}
		hashedPassword, hashErr := HashPassword(randomPassword)
		if hashErr != nil {
			return nil, fmt.Errorf("failed to hash temp password: %w", hashErr)
		}

		// Mark as verified since they're being admin-provisioned
		_, err = tx.Exec(
			"INSERT INTO users (email, password, is_verified, is_admin, created_at, updated_at) VALUES (?, ?, 1, 0, ?, ?)",
			firstAdminEmail, hashedPassword, now, now,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create admin user: %w", err)
		}
		// Get the newly created user's ID
		var adminUserIDVal int64
		err = tx.QueryRow("SELECT LAST_INSERT_ID()").Scan(&adminUserIDVal)
		if err != nil {
			return nil, fmt.Errorf("failed to get admin user ID: %w", err)
		}
		adminUserID = uint(adminUserIDVal)
	} else if err != nil {
		return nil, fmt.Errorf("failed to check existing user: %w", err)
	} else {
		// User exists — get their ID
		err = tx.QueryRow("SELECT id FROM users WHERE email = ? LIMIT 1", firstAdminEmail).Scan(&adminUserID)
		if err != nil {
			return nil, fmt.Errorf("failed to get admin user ID: %w", err)
		}
	}

	// Add first admin as org member
	_, err = tx.Exec(
		"INSERT INTO organization_members (organization_id, user_id, role, is_active, joined_at, updated_at) VALUES (?, ?, ?, 1, ?, ?)",
		orgID, adminUserID, firstAdminRole, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to add first admin: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit organization creation: %w", err)
	}

	return &models.Organization{
		ID:        uint(orgID),
		UUID:      uuid,
		Name:      name,
		Slug:      slug,
		CreatedBy: createdBy,
		IsActive:  true,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// GetOrganizationByID retrieves an organization by its ID.
func GetOrganizationByID(id uint) (*models.Organization, error) {
	var org models.Organization
	var createdByInt int64
	err := models.DB.QueryRow(
		"SELECT id, uuid, name, slug, created_by, is_active, created_at, updated_at FROM organizations WHERE id = ? LIMIT 1",
		id,
	).Scan(&org.ID, &org.UUID, &org.Name, &org.Slug, &createdByInt, &org.IsActive, &org.CreatedAt, &org.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, errors.New("organization not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get organization: %w", err)
	}
	org.CreatedBy = uint(createdByInt)
	return &org, nil
}

// GetOrganizationBySlug retrieves an organization by its slug.
func GetOrganizationBySlug(slug string) (*models.Organization, error) {
	var org models.Organization
	var createdByInt int64
	err := models.DB.QueryRow(
		"SELECT id, uuid, name, slug, created_by, is_active, created_at, updated_at FROM organizations WHERE slug = ? LIMIT 1",
		slug,
	).Scan(&org.ID, &org.UUID, &org.Name, &org.Slug, &createdByInt, &org.IsActive, &org.CreatedAt, &org.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, errors.New("organization not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get organization: %w", err)
	}
	org.CreatedBy = uint(createdByInt)
	return &org, nil
}

// ListOrganizationsByUser retrieves all organizations a user belongs to.
func ListOrganizationsByUser(userID uint) ([]*models.Organization, error) {
	rows, err := models.DB.Query(
		`SELECT o.id, o.uuid, o.name, o.slug, o.created_by, o.is_active, o.created_at, o.updated_at
		 FROM organizations o
		 INNER JOIN organization_members om ON o.id = om.organization_id
		 WHERE om.user_id = ? AND om.is_active = 1
		 ORDER BY o.created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list organizations: %w", err)
	}
	defer rows.Close()

	var orgs []*models.Organization
	for rows.Next() {
		var org models.Organization
		var createdByInt int64
		err := rows.Scan(&org.ID, &org.UUID, &org.Name, &org.Slug, &createdByInt, &org.IsActive, &org.CreatedAt, &org.UpdatedAt)
		if err != nil {
			continue
		}
		org.CreatedBy = uint(createdByInt)
		orgs = append(orgs, &org)
	}
	return orgs, nil
}

// ─── Organization Membership ───────────────────────────────────────

// AddOrganizationMember adds a user to an organization.
// Returns the created membership record.
func AddOrganizationMember(orgID, userID uint, role string) (*models.OrganizationMember, error) {
	if !models.IsValidOrgRole(role) {
		return nil, fmt.Errorf("invalid org role: %s", role)
	}

	now := time.Now().UTC()
	result, err := models.DB.Exec(
		"INSERT INTO organization_members (organization_id, user_id, role, is_active, joined_at, updated_at) VALUES (?, ?, ?, 1, ?, ?)",
		orgID, userID, role, now, now,
	)
	if err != nil {
		if isDuplicateKeyError(err) {
			return nil, errors.New("user is already a member of this organization")
		}
		return nil, fmt.Errorf("failed to add member: %w", err)
	}

	memberID, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get member ID: %w", err)
	}

	return &models.OrganizationMember{
		ID:             uint(memberID),
		OrganizationID: orgID,
		UserID:         userID,
		Role:           role,
		IsActive:       true,
		JoinedAt:       now,
		UpdatedAt:      now,
	}, nil
}

// RemoveOrganizationMember removes a user from an organization.
func RemoveOrganizationMember(orgID, userID uint) error {
	_, err := models.DB.Exec(
		"UPDATE organization_members SET is_active = 0, updated_at = CURRENT_TIMESTAMP WHERE organization_id = ? AND user_id = ?",
		orgID, userID,
	)
	if err != nil {
		return fmt.Errorf("failed to remove member: %w", err)
	}
	return nil
}

// UpdateOrganizationMemberRole updates a member's role within an organization.
func UpdateOrganizationMemberRole(orgID, userID uint, newRole string) (*models.OrganizationMember, error) {
	if !models.IsValidOrgRole(newRole) {
		return nil, fmt.Errorf("invalid org role: %s", newRole)
	}

	now := time.Now().UTC()
	result, err := models.DB.Exec(
		"UPDATE organization_members SET role = ?, updated_at = ? WHERE organization_id = ? AND user_id = ?",
		newRole, now, orgID, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to update member role: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil || rowsAffected == 0 {
		return nil, errors.New("member not found")
	}

	return &models.OrganizationMember{
		OrganizationID: orgID,
		UserID:         userID,
		Role:           newRole,
		IsActive:       true,
		UpdatedAt:      now,
	}, nil
}

// TransferOrganizationOwnership transfers ownership of an organization.
// newOwnerID must already be an org admin.
func TransferOrganizationOwnership(orgID, newOwnerID uint) error {
	tx, err := models.DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	// Verify new owner is an org admin
	var currentRole string
	err = tx.QueryRow(
		"SELECT role FROM organization_members WHERE organization_id = ? AND user_id = ? AND is_active = 1",
		orgID, newOwnerID,
	).Scan(&currentRole)
	if err == sql.ErrNoRows {
		return errors.New("new owner is not a member of this organization")
	}
	if err != nil {
		return fmt.Errorf("failed to check new owner role: %w", err)
	}
	if currentRole != "admin" {
		return errors.New("new owner must be an org admin")
	}

	now := time.Now().UTC()
	_, err = tx.Exec(
		"UPDATE organization_members SET role = 'owner', updated_at = ? WHERE organization_id = ? AND user_id = ?",
		now, orgID, newOwnerID,
	)
	if err != nil {
		return fmt.Errorf("failed to update new owner role: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit ownership transfer: %w", err)
	}

	return nil
}

// DeleteOrganization soft-deletes an organization.
// All members are deactivated but not removed.
func DeleteOrganization(orgID uint) error {
	now := time.Now().UTC()
	_, err := models.DB.Exec(
		"UPDATE organizations SET is_active = 0, updated_at = ? WHERE id = ?",
		now, orgID,
	)
	if err != nil {
		return fmt.Errorf("failed to delete organization: %w", err)
	}

	// Deactivate all members
	_, err = models.DB.Exec(
		"UPDATE organization_members SET is_active = 0, updated_at = ? WHERE organization_id = ?",
		now, orgID,
	)
	if err != nil {
		return fmt.Errorf("failed to deactivate org members: %w", err)
	}

	return nil
}

// IsOrganizationMember checks if a user is an active member of an organization.
func IsOrganizationMember(orgID, userID uint) (bool, error) {
	var count int
	err := models.DB.QueryRow(
		"SELECT COUNT(*) FROM organization_members WHERE organization_id = ? AND user_id = ? AND is_active = 1",
		orgID, userID,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// IsOrganizationAdmin checks if a user has admin or owner role in an organization.
func IsOrganizationAdmin(orgID, userID uint) (bool, error) {
	var count int
	err := models.DB.QueryRow(
		"SELECT COUNT(*) FROM organization_members WHERE organization_id = ? AND user_id = ? AND is_active = 1 AND role IN ('owner', 'admin')",
		orgID, userID,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// IsOrganizationOwner checks if a user has owner role in an organization.
func IsOrganizationOwner(orgID, userID uint) (bool, error) {
	var count int
	err := models.DB.QueryRow(
		"SELECT COUNT(*) FROM organization_members WHERE organization_id = ? AND user_id = ? AND is_active = 1 AND role = 'owner'",
		orgID, userID,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// GetOrganizationMemberRole returns the role of a user within an organization.
func GetOrganizationMemberRole(orgID, userID uint) (string, error) {
	var role string
	err := models.DB.QueryRow(
		"SELECT role FROM organization_members WHERE organization_id = ? AND user_id = ? AND is_active = 1",
		orgID, userID,
	).Scan(&role)
	if err == sql.ErrNoRows {
		return "", errors.New("user is not a member of this organization")
	}
	if err != nil {
		return "", fmt.Errorf("failed to get member role: %w", err)
	}
	return role, nil
}

// GetOrganizationMemberCount returns the number of active members in an organization.
func GetOrganizationMemberCount(orgID uint) (int, error) {
	var count int
	err := models.DB.QueryRow(
		"SELECT COUNT(*) FROM organization_members WHERE organization_id = ? AND is_active = 1",
		orgID,
	).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// LeaveOrganization removes the current user from an organization.
// For org owners, they must first transfer ownership.
func LeaveOrganization(orgID, userID uint) error {
	// Check if user is the last owner
	isOwner, err := IsOrganizationOwner(orgID, userID)
	if err != nil {
		return fmt.Errorf("failed to check ownership: %w", err)
	}
	if isOwner {
		// Count other owners
		var ownerCount int
		err = models.DB.QueryRow(
			"SELECT COUNT(*) FROM organization_members WHERE organization_id = ? AND role = 'owner' AND is_active = 1 AND user_id != ?",
			orgID, userID,
		).Scan(&ownerCount)
		if err != nil {
			return fmt.Errorf("failed to count other owners: %w", err)
		}
		if ownerCount <= 1 {
			return errors.New("cannot leave organization: you are the last owner. Transfer ownership first.")
		}
	}

	return RemoveOrganizationMember(orgID, userID)
}

// ─── Helper ────────────────────────────────────────────────────────

// isDuplicateKeyError checks if an error is a MySQL duplicate key error.
func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	// MySQL error 1062: Duplicate entry
	return err.Error() == "Error 1062: Duplicate entry" ||
		err.Error() == "Error 1062: Duplicate entry" ||
		err.Error() == "sql: expected 1 destination argument in Scan, not 0"
}
