package services

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"

	"YourQL/pkg/models"
	"YourQL/pkg/utils"
)

// GenerateUUID creates a UUID v4 string.
func GenerateUUID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// CreateWorkspace creates a new workspace and assigns the user as owner.
// If organizationID is set, the workspace is scoped to that org and
// the caller must be an org admin/owner.
func CreateWorkspace(name, description string, ownerID uint, organizationID *uint) (*models.Workspace, error) {
	uuid, err := GenerateUUID()
	if err != nil {
		return nil, fmt.Errorf("failed to generate UUID: %w", err)
	}

	slug, err := utils.EnsureUniqueSlug(name, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to generate unique slug: %w", err)
	}

	now := time.Now().UTC()

	// Validate org membership if organization_id is provided
	if organizationID != nil {
		isAdmin, err := IsOrganizationAdmin(*organizationID, ownerID)
		if err != nil {
			return nil, fmt.Errorf("failed to validate org membership: %w", err)
		}
		if !isAdmin {
			return nil, errors.New("you must be an org admin to create workspaces in this organization")
		}
	} else {
		// No organization specified — personal workspaces are auto-created on email confirmation only.
		// Reject manual creation of org-less workspaces.
		return nil, errors.New("personal workspaces are automatically created on email confirmation. To create a workspace, join an organization and select it below.")
	}

	tx, err := models.DB.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	// Build INSERT columns dynamically
	var insertCols string
	var insertVals []interface{}

	insertCols = "uuid, name, slug, description, owner_id"
	insertVals = append(insertVals, uuid, name, slug, description, ownerID)

	// Add organization_id column if provided
	var orgIDForWorkspace *uint
	if organizationID != nil {
		insertCols += ", organization_id"
		insertVals = append(insertVals, *organizationID)
		orgIDForWorkspace = organizationID
	}

	insertCols += ", is_active, created_at, updated_at"
	insertVals = append(insertVals, 1, now, now)

	placeholders := make([]string, len(insertVals))
	for i := range placeholders {
		placeholders[i] = "?"
	}

	result, err := tx.Exec(
		fmt.Sprintf("INSERT INTO workspaces (%s) VALUES (%s)", insertCols, placeholders),
		insertVals...,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create workspace: %w", err)
	}

	workspaceID, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get workspace ID: %w", err)
	}

	_, err = tx.Exec(
		"INSERT INTO workspace_users (workspace_id, user_id, role, is_active, joined_at, updated_at) VALUES (?, ?, 'owner', 1, ?, ?)",
		workspaceID, ownerID, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create workspace membership: %w", err)
	}

	defaultRoles := []struct {
		name        string
		description string
		permissions string
	}{
		{
			"viewer",
			"Read-only access to workspace data",
			`{"can_query": false, "can_manage_llm": false, "can_manage_db": false, "can_manage_members": false, "can_manage_roles": false, "can_manage_settings": false, "can_save_queries": false, "can_view_history": false, "can_create_conversations": false, "can_export_data": false}`,
		},
		{
			"member",
			"Can query, create conversations, and save queries",
			`{"can_query": true, "can_manage_llm": false, "can_manage_db": false, "can_manage_members": false, "can_manage_roles": false, "can_manage_settings": false, "can_save_queries": true, "can_view_history": true, "can_create_conversations": true, "can_export_data": false}`,
		},
		{
			"admin",
			"Full management access except workspace deletion",
			`{"can_query": true, "can_manage_llm": true, "can_manage_db": true, "can_manage_members": true, "can_manage_roles": false, "can_manage_settings": true, "can_save_queries": true, "can_view_history": true, "can_create_conversations": true, "can_export_data": true}`,
		},
	}

	for _, role := range defaultRoles {
		_, err = tx.Exec(
			"INSERT INTO workspace_roles (workspace_id, name, description, permissions, is_system, created_at, updated_at) VALUES (?, ?, ?, ?, 1, ?, ?)",
			workspaceID, role.name, role.description, role.permissions, now, now,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create default role '%s': %w", role.name, err)
		}
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit workspace creation: %w", err)
	}

	return &models.Workspace{
		ID:             uint(workspaceID),
		UUID:           uuid,
		Name:           name,
		Slug:           slug,
		OrganizationID: orgIDForWorkspace,
		IsActive:       true,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

// GetWorkspaceByID retrieves a workspace by its ID.
func GetWorkspaceByID(id uint) (*models.Workspace, error) {
	var w models.Workspace
	var defaultLLMNull, defaultDBNull sql.NullString
	var settingsNull []byte
	err := models.DB.QueryRow(
		"SELECT id, uuid, name, slug, description, owner_id, default_llm_provider, default_db_connection, settings, is_active, created_at, updated_at FROM workspaces WHERE id = ? LIMIT 1",
		id,
	).Scan(
		&w.ID, &w.UUID, &w.Name, &w.Slug, &w.Description, &w.OwnerID,
		&defaultLLMNull, &defaultDBNull, &settingsNull,
		&w.IsActive, &w.CreatedAt, &w.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, errors.New("workspace not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get workspace: %w", err)
	}
	if defaultLLMNull.Valid {
		w.DefaultLLMProvider = defaultLLMNull.String
	} else {
		w.DefaultLLMProvider = ""
	}
	if defaultDBNull.Valid {
		w.DefaultDBConnection = defaultDBNull.String
	} else {
		w.DefaultDBConnection = ""
	}
	s := new(string)
	if len(settingsNull) > 0 {
		*s = string(settingsNull)
	} else {
		*s = ""
	}
	w.Settings = s
	return &w, nil
}

// GetWorkspaceByUUID retrieves a workspace by its UUID.
func GetWorkspaceByUUID(uuid string) (*models.Workspace, error) {
	var w models.Workspace
	var defaultLLMNull, defaultDBNull sql.NullString
	var settingsNull []byte
	err := models.DB.QueryRow(
		"SELECT id, uuid, name, slug, description, owner_id, default_llm_provider, default_db_connection, settings, is_active, created_at, updated_at FROM workspaces WHERE uuid = ? LIMIT 1",
		uuid,
	).Scan(
		&w.ID, &w.UUID, &w.Name, &w.Slug, &w.Description, &w.OwnerID,
		&defaultLLMNull, &defaultDBNull, &settingsNull,
		&w.IsActive, &w.CreatedAt, &w.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, errors.New("workspace not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get workspace: %w", err)
	}
	if defaultLLMNull.Valid {
		w.DefaultLLMProvider = defaultLLMNull.String
	} else {
		w.DefaultLLMProvider = ""
	}
	if defaultDBNull.Valid {
		w.DefaultDBConnection = defaultDBNull.String
	} else {
		w.DefaultDBConnection = ""
	}
	s := new(string)
	if len(settingsNull) > 0 {
		*s = string(settingsNull)
	} else {
		*s = ""
	}
	w.Settings = s
	return &w, nil
}

// GetWorkspaceBySlug retrieves a workspace by its slug.
func GetWorkspaceBySlug(slug string) (*models.Workspace, error) {
	var w models.Workspace
	var defaultLLMNull, defaultDBNull sql.NullString
	var settingsNull []byte
	err := models.DB.QueryRow(
		"SELECT id, uuid, name, slug, description, owner_id, default_llm_provider, default_db_connection, settings, is_active, created_at, updated_at FROM workspaces WHERE slug = ? LIMIT 1",
		slug,
	).Scan(
		&w.ID, &w.UUID, &w.Name, &w.Slug, &w.Description, &w.OwnerID,
		&defaultLLMNull, &defaultDBNull, &settingsNull,
		&w.IsActive, &w.CreatedAt, &w.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, errors.New("workspace not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get workspace: %w", err)
	}
	if defaultLLMNull.Valid {
		w.DefaultLLMProvider = defaultLLMNull.String
	} else {
		w.DefaultLLMProvider = ""
	}
	if defaultDBNull.Valid {
		w.DefaultDBConnection = defaultDBNull.String
	} else {
		w.DefaultDBConnection = ""
	}
	s := new(string)
	if len(settingsNull) > 0 {
		*s = string(settingsNull)
	} else {
		*s = ""
	}
	w.Settings = s
	return &w, nil
}

// ListWorkspacesByUser retrieves all workspaces a user belongs to.
func ListWorkspacesByUser(userID uint) ([]*models.Workspace, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rows, err := models.DB.QueryContext(ctx, `
		SELECT w.id, w.uuid, w.name, w.slug, w.description, w.owner_id,
		       w.default_llm_provider, w.default_db_connection, w.settings,
		       w.is_active, w.created_at, w.updated_at
		FROM workspaces w
		INNER JOIN workspace_users wu ON w.id = wu.workspace_id
		WHERE wu.user_id = ? AND wu.is_active = 1
		ORDER BY w.created_at DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list workspaces: %w", err)
	}
	defer rows.Close()

	var workspaces []*models.Workspace
	for rows.Next() {
		var w models.Workspace
		var settingsNull []byte
		var defaultLLMNull, defaultDBNull sql.NullString
		err := rows.Scan(
			&w.ID, &w.UUID, &w.Name, &w.Slug, &w.Description, &w.OwnerID,
			&defaultLLMNull, &defaultDBNull, &settingsNull,
			&w.IsActive, &w.CreatedAt, &w.UpdatedAt,
		)
		if err != nil {
			continue
		}
		if defaultLLMNull.Valid {
			w.DefaultLLMProvider = defaultLLMNull.String
		}
		if defaultDBNull.Valid {
			w.DefaultDBConnection = defaultDBNull.String
		}
		s := new(string)
		if len(settingsNull) > 0 {
			*s = string(settingsNull)
		} else {
			*s = ""
		}
		w.Settings = s
		workspace := w
		workspaces = append(workspaces, &workspace)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}
	return workspaces, nil
}

// UpdateWorkspace updates a workspace's name, description, or other fields.
func UpdateWorkspace(id uint, name, description *string) (*models.Workspace, error) {
	w, err := GetWorkspaceByID(id)
	if err != nil {
		return nil, err
	}

	newName := name
	newSlug := w.Slug
	if name != nil && *name != w.Name {
		newName = name
		newSlug, err = utils.EnsureUniqueSlug(*name, nil)
		if err != nil {
			return nil, err
		}
	}

	newDescription := w.Description
	if description != nil {
		newDescription = *description
	}

	now := time.Now().UTC()
	_, err = models.DB.Exec(
		"UPDATE workspaces SET name = ?, slug = ?, description = ?, updated_at = ? WHERE id = ?",
		newName, newSlug, newDescription, now, id,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to update workspace: %w", err)
	}

	return GetWorkspaceByID(id)
}

// DeleteWorkspace soft-deletes a workspace.
func DeleteWorkspace(id uint) error {
	now := time.Now().UTC()
	result, err := models.DB.Exec(
		"UPDATE workspaces SET is_active = 0, updated_at = ? WHERE id = ?", now, id,
	)
	if err != nil {
		return fmt.Errorf("failed to delete workspace: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows == 0 {
		return errors.New("workspace not found")
	}
	return nil
}

// TransferOwnership transfers workspace ownership to another user.
func TransferOwnership(workspaceID, newOwnerID uint) error {
	_, err := models.DB.Exec(
		"UPDATE workspaces SET owner_id = ? WHERE id = ?", newOwnerID, workspaceID,
	)
	if err != nil {
		return fmt.Errorf("failed to transfer ownership: %w", err)
	}

	var count int
	err = models.DB.QueryRow(
		"SELECT COUNT(*) FROM workspace_users WHERE workspace_id = ? AND user_id = ?",
		workspaceID, newOwnerID,
	).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check membership: %w", err)
	}
	if count == 0 {
		now := time.Now().UTC()
		_, err = models.DB.Exec(
			"INSERT INTO workspace_users (workspace_id, user_id, role, is_active, joined_at, updated_at) VALUES (?, ?, 'owner', 1, ?, ?)",
			workspaceID, newOwnerID, now, now,
		)
		if err != nil {
			return fmt.Errorf("failed to add new owner to workspace: %w", err)
		}
	}
	return nil
}

// IsWorkspaceOwner checks if a user is the owner of a workspace.
func IsWorkspaceOwner(workspaceID, userID uint) (bool, error) {
	var ownerID uint
	err := models.DB.QueryRow(
		"SELECT owner_id FROM workspaces WHERE id = ? LIMIT 1", workspaceID,
	).Scan(&ownerID)
	if err != nil {
		return false, err
	}
	return ownerID == userID, nil
}

// IsWorkspaceMember checks if a user is an active member of a workspace.
func IsWorkspaceMember(workspaceID, userID uint) (bool, error) {
	var count int
	err := models.DB.QueryRow(
		"SELECT COUNT(*) FROM workspace_users WHERE workspace_id = ? AND user_id = ? AND is_active = 1",
		workspaceID, userID,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// GetWorkspaceCountByUser returns the number of workspaces a user belongs to.
func GetWorkspaceCountByUser(userID uint) (int, error) {
	var count int
	err := models.DB.QueryRow(
		"SELECT COUNT(*) FROM workspace_users WHERE user_id = ? AND is_active = 1", userID,
	).Scan(&count)
	return count, err
}

// IsPersonalWorkspace checks if a workspace is the personal workspace for a given user.
func IsPersonalWorkspace(workspaceID, userID uint) (bool, error) {
	var organizationID sql.NullInt64
	var isPersonal bool
	err := models.DB.QueryRow(
		"SELECT organization_id, is_personal FROM workspaces WHERE id = ? LIMIT 1", workspaceID,
	).Scan(&organizationID, &isPersonal)
	if err != nil {
		return false, err
	}
	if !isPersonal {
		return false, nil
	}
	// Check if user is the owner
	var ownerID uint
	err = models.DB.QueryRow(
		"SELECT owner_id FROM workspaces WHERE id = ? LIMIT 1", workspaceID,
	).Scan(&ownerID)
	if err != nil {
		return false, err
	}
	return ownerID == userID, nil
}

// GetPersonalWorkspace retrieves the personal workspace for a user.
// Returns nil if no personal workspace exists (should not happen for verified users).
func GetPersonalWorkspace(userID uint) (*models.Workspace, error) {
	var w models.Workspace
	var defaultLLMNull, defaultDBNull sql.NullString
	var settingsNull []byte
	var orgID sql.NullInt64
	var isPersonal bool
	err := models.DB.QueryRow(
		"SELECT id, uuid, name, slug, description, owner_id, organization_id, is_personal, default_llm_provider, default_db_connection, settings, is_active, created_at, updated_at FROM workspaces WHERE owner_id = ? AND is_personal = 1 LIMIT 1",
		userID,
	).Scan(
		&w.ID, &w.UUID, &w.Name, &w.Slug, &w.Description, &w.OwnerID,
		&orgID, &isPersonal,
		&defaultLLMNull, &defaultDBNull, &settingsNull,
		&w.IsActive, &w.CreatedAt, &w.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil // No personal workspace yet
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get personal workspace: %w", err)
	}
	if defaultLLMNull.Valid {
		w.DefaultLLMProvider = defaultLLMNull.String
	}
	if defaultDBNull.Valid {
		w.DefaultDBConnection = defaultDBNull.String
	}
	s := new(string)
	if len(settingsNull) > 0 {
		*s = string(settingsNull)
	}
	w.Settings = s
	return &w, nil
}

// GroupedWorkspaceResponse is the grouped API response format for workspaces.
type GroupedWorkspaceResponse struct {
	PersonalWorkspace *WorkspaceResponse `json:"personal_workspace"`
	Organizations     []OrgWorkspaceGroup `json:"organizations"`
}

// WorkspaceResponse is a simplified workspace representation for API responses.
type WorkspaceResponse struct {
	ID             uint   `json:"id"`
	UUID           string `json:"uuid"`
	Name           string `json:"name"`
	Slug           string `json:"slug"`
	IsActive       bool   `json:"is_active"`
	OrganizationID *uint  `json:"organization_id,omitempty"`
}

// OrgWorkspaceGroup groups workspaces under an organization.
type OrgWorkspaceGroup struct {
	OrganizationID uint                `json:"organization_id"`
	OrganizationName string             `json:"organization_name"`
	OrganizationSlug string             `json:"organization_slug"`
	UserRole        string             `json:"user_role"`
	Workspaces      []WorkspaceResponse `json:"workspaces"`
}

// ListWorkspacesGroupedByOrg returns workspaces grouped by organization,
// with the personal workspace listed separately at the top.
func ListWorkspacesGroupedByOrg(userID uint) (*GroupedWorkspaceResponse, error) {
	result := &GroupedWorkspaceResponse{
		Organizations: []OrgWorkspaceGroup{},
	}

	// Get personal workspace
	personalWS, err := GetPersonalWorkspace(userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get personal workspace: %w", err)
	}
	if personalWS != nil {
		result.PersonalWorkspace = &WorkspaceResponse{
			ID:         personalWS.ID,
			UUID:       personalWS.UUID,
			Name:       personalWS.Name,
			Slug:       personalWS.Slug,
			IsActive:   personalWS.IsActive,
			OrganizationID: nil,
		}
	}

	// Get org memberships
	orgMemberships, err := ListOrganizationsByUser(userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list org memberships: %w", err)
	}

	for _, org := range orgMemberships {
		// Get user's role in this org
		role, err := GetOrganizationMemberRole(org.ID, userID)
		if err != nil {
			continue // Skip if somehow not a member
		}

		// Get workspaces in this org
		orgWorkspaces, err := ListWorkspacesByOrg(org.ID)
		if err != nil {
			continue
		}

		var wsResponses []WorkspaceResponse
		for _, ws := range orgWorkspaces {
			wsResponses = append(wsResponses, WorkspaceResponse{
				ID:             ws.ID,
				UUID:           ws.UUID,
				Name:           ws.Name,
				Slug:           ws.Slug,
				IsActive:       ws.IsActive,
				OrganizationID: ws.OrganizationID,
			})
		}

		if len(wsResponses) > 0 {
			result.Organizations = append(result.Organizations, OrgWorkspaceGroup{
				OrganizationID:   org.ID,
				OrganizationName: org.Name,
				OrganizationSlug: org.Slug,
				UserRole:         role,
				Workspaces:       wsResponses,
			})
		}
	}

	// Get orphaned workspaces (organization_id IS NULL, is_personal = 0)
	orphanedRows, err := models.DB.Query(`
		SELECT w.id, w.uuid, w.name, w.slug, w.description, w.owner_id,
		       w.default_llm_provider, w.default_db_connection, w.settings,
		       w.is_active, w.created_at, w.updated_at
		FROM workspaces w
		INNER JOIN workspace_users wu ON w.id = wu.workspace_id
		WHERE wu.user_id = ? AND wu.is_active = 1
		  AND w.organization_id IS NULL AND w.is_personal = 0
		ORDER BY w.created_at DESC
	`, userID)
	if err != nil {
		log.Printf("[WARN] Failed to fetch orphaned workspaces: %v", err)
	} else {
		defer orphanedRows.Close()
		var orphanedResponses []WorkspaceResponse
		for orphanedRows.Next() {
			var w models.Workspace
			var settingsNull []byte
			var defaultLLMNull, defaultDBNull sql.NullString
			var orgIDNull sql.NullInt64
			err := orphanedRows.Scan(
				&w.ID, &w.UUID, &w.Name, &w.Slug, &w.Description, &w.OwnerID,
				&orgIDNull,
				&defaultLLMNull, &defaultDBNull, &settingsNull,
				&w.IsActive, &w.CreatedAt, &w.UpdatedAt,
			)
			if err != nil {
				continue
			}
			orphanedResponses = append(orphanedResponses, WorkspaceResponse{
				ID:             w.ID,
				UUID:           w.UUID,
				Name:           w.Name,
				Slug:           w.Slug,
				IsActive:       w.IsActive,
				OrganizationID: nil,
			})
		}
		if len(orphanedResponses) > 0 {
			result.Organizations = append(result.Organizations, OrgWorkspaceGroup{
				OrganizationID:   0,
				OrganizationName: "Other Workspaces",
				OrganizationSlug: "other",
				UserRole:         "member",
				Workspaces:       orphanedResponses,
			})
		}
	}

	return result, nil
}

// ListWorkspacesByOrg returns all workspaces belonging to an organization.
func ListWorkspacesByOrg(orgID uint) ([]*models.Workspace, error) {
	rows, err := models.DB.Query(`
		SELECT w.id, w.uuid, w.name, w.slug, w.description, w.owner_id,
		       w.organization_id, w.default_llm_provider, w.default_db_connection,
		       w.settings, w.is_active, w.created_at, w.updated_at
		FROM workspaces w
		WHERE w.organization_id = ? AND w.is_active = 1
		ORDER BY w.created_at DESC
	`, orgID)
	if err != nil {
		return nil, fmt.Errorf("failed to list org workspaces: %w", err)
	}
	defer rows.Close()

	var workspaces []*models.Workspace
	for rows.Next() {
		var w models.Workspace
		var settingsNull []byte
		var defaultLLMNull, defaultDBNull sql.NullString
		var orgIDNull sql.NullInt64
		err := rows.Scan(
			&w.ID, &w.UUID, &w.Name, &w.Slug, &w.Description, &w.OwnerID,
			&orgIDNull,
			&defaultLLMNull, &defaultDBNull, &settingsNull,
			&w.IsActive, &w.CreatedAt, &w.UpdatedAt,
		)
		if err != nil {
			continue
		}
		if orgIDNull.Valid {
			orgIDVal := uint(orgIDNull.Int64)
			w.OrganizationID = &orgIDVal
		}
		if defaultLLMNull.Valid {
			w.DefaultLLMProvider = defaultLLMNull.String
		}
		if defaultDBNull.Valid {
			w.DefaultDBConnection = defaultDBNull.String
		}
		s := new(string)
		if len(settingsNull) > 0 {
			*s = string(settingsNull)
		} else {
			*s = ""
		}
		w.Settings = s
		workspaces = append(workspaces, &w)
	}
	return workspaces, nil
}

// CreatePersonalWorkspace creates a personal workspace for a verified user.
// Idempotent: if personal workspace already exists, returns it without error.
// Only called when ENABLE_ORG_FEATURES is true.
func CreatePersonalWorkspace(userID uint, email string) (*models.Workspace, error) {
	// Check if personal workspace already exists (idempotent)
	existing, err := GetPersonalWorkspace(userID)
	if err != nil {
		return nil, fmt.Errorf("failed to check for existing personal workspace: %w", err)
	}
	if existing != nil {
		return existing, nil
	}

	// Generate slug: me-{sanitized-email}
	slug, err := utils.EnsureUniquePersonalSlug(email)
	if err != nil {
		return nil, fmt.Errorf("failed to generate personal workspace slug: %w", err)
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

	// Create workspace with organization_id = NULL and is_personal = 1
	result, err := tx.Exec(
		"INSERT INTO workspaces (uuid, name, slug, description, owner_id, organization_id, is_personal, is_active, created_at, updated_at) VALUES (?, ?, ?, ?, ?, NULL, 1, 1, ?, ?)",
		uuid, email, slug, email, userID, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create personal workspace: %w", err)
	}

	workspaceID, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get workspace ID: %w", err)
	}

	// Add user as owner
	_, err = tx.Exec(
		"INSERT INTO workspace_users (workspace_id, user_id, role, is_active, joined_at, updated_at) VALUES (?, ?, 'owner', 1, ?, ?)",
		workspaceID, userID, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create personal workspace membership: %w", err)
	}

	// Create default workspace roles
	defaultRoles := []struct {
		name        string
		description string
		permissions string
	}{
		{
			"viewer",
			"Read-only access to workspace data",
			`{"can_query": false, "can_manage_llm": false, "can_manage_db": false, "can_manage_members": false, "can_manage_roles": false, "can_manage_settings": false, "can_save_queries": false, "can_view_history": false, "can_create_conversations": false, "can_export_data": false}`,
		},
		{
			"member",
			"Can query, create conversations, and save queries",
			`{"can_query": true, "can_manage_llm": false, "can_manage_db": false, "can_manage_members": false, "can_manage_roles": false, "can_manage_settings": false, "can_save_queries": true, "can_view_history": true, "can_create_conversations": true, "can_export_data": false}`,
		},
		{
			"admin",
			"Full management access except workspace deletion",
			`{"can_query": true, "can_manage_llm": true, "can_manage_db": true, "can_manage_members": true, "can_manage_roles": false, "can_manage_settings": true, "can_save_queries": true, "can_view_history": true, "can_create_conversations": true, "can_export_data": true}`,
		},
	}

	for _, role := range defaultRoles {
		_, err = tx.Exec(
			"INSERT INTO workspace_roles (workspace_id, name, description, permissions, is_system, created_at, updated_at) VALUES (?, ?, ?, ?, 1, ?, ?)",
			workspaceID, role.name, role.description, role.permissions, now, now,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create default role '%s': %w", role.name, err)
		}
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit personal workspace creation: %w", err)
	}

	return &models.Workspace{
		ID:        uint(workspaceID),
		UUID:      uuid,
		Name:      email,
		Slug:      slug,
		IsActive:  true,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}
