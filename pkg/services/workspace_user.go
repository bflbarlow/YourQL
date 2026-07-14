package services

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"YourQL/pkg/configuration"
	"YourQL/pkg/models"

	"github.com/gin-gonic/gin"
)

func AddWorkspaceMember(workspaceID, userID uint, role string) error {
	if role == "" {
		role = configuration.Config.Workspace.DefaultRole
	}
	if role == "" {
		role = "member"
	}

	now := time.Now().UTC()
	_, err := models.DB.Exec(
		"INSERT INTO workspace_users (workspace_id, user_id, role, is_active, joined_at, updated_at) VALUES (?, ?, ?, 1, ?, ?)",
		workspaceID, userID, role, now, now,
	)
	if err != nil {
		if isDuplicateEntry(err) {
			return errors.New("user is already a member of this workspace")
		}
		return fmt.Errorf("failed to add workspace member: %w", err)
	}
	return nil
}

func RemoveWorkspaceMember(workspaceID, userID uint) error {
	isOwner, err := IsWorkspaceOwner(workspaceID, userID)
	if err != nil {
		return err
	}
	if isOwner {
		return errors.New("cannot remove the workspace owner")
	}

	_, err = models.DB.Exec(
		"UPDATE workspace_users SET is_active = 0, updated_at = CURRENT_TIMESTAMP WHERE workspace_id = ? AND user_id = ?",
		workspaceID, userID,
	)
	if err != nil {
		return fmt.Errorf("failed to remove workspace member: %w", err)
	}
	return nil
}

func UpdateWorkspaceMemberRole(workspaceID, userID uint, newRole string) error {
	if newRole == "" {
		return errors.New("role cannot be empty")
	}

	now := time.Now().UTC()
	result, err := models.DB.Exec(
		"UPDATE workspace_users SET role = ?, updated_at = ? WHERE workspace_id = ? AND user_id = ?",
		newRole, now, workspaceID, userID,
	)
	if err != nil {
		return fmt.Errorf("failed to update member role: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows == 0 {
		return errors.New("member not found")
	}
	return nil
}

func GetWorkspaceMember(workspaceID, userID uint) (*models.WorkspaceUser, error) {
	var su models.WorkspaceUser
	err := models.DB.QueryRow(
		"SELECT id, workspace_id, user_id, role, is_active, joined_at, updated_at FROM workspace_users WHERE workspace_id = ? AND user_id = ? LIMIT 1",
		workspaceID, userID,
	).Scan(
		&su.ID, &su.WorkspaceID, &su.UserID, &su.Role, &su.IsActive,
		&su.JoinedAt, &su.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, errors.New("member not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get workspace member: %w", err)
	}
	return &su, nil
}

func ListWorkspaceMembers(workspaceID uint) ([]*models.WorkspaceUser, error) {
	rows, err := models.DB.Query(
		"SELECT id, workspace_id, user_id, role, is_active, joined_at, updated_at FROM workspace_users WHERE workspace_id = ? AND is_active = 1 ORDER BY joined_at DESC",
		workspaceID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list workspace members: %w", err)
	}
	defer rows.Close()

	var members []*models.WorkspaceUser
	for rows.Next() {
		var su models.WorkspaceUser
		err := rows.Scan(
			&su.ID, &su.WorkspaceID, &su.UserID, &su.Role, &su.IsActive,
			&su.JoinedAt, &su.UpdatedAt,
		)
		if err != nil {
			continue
		}
		members = append(members, &su)
	}
	return members, nil
}

func CreateWorkspaceInvitation(workspaceID uint, email, role string, invitedBy uint) (*models.WorkspaceInvitation, error) {
	if role == "" {
		role = configuration.Config.Workspace.DefaultRole
	}
	if role == "" {
		role = "member"
	}

	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("failed to generate invitation token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)

	expiresAt := time.Now().UTC().Add(7 * 24 * time.Hour)

	_, err := models.DB.Exec(
		"INSERT INTO workspace_invitations (workspace_id, email, role, invited_by, token, expires_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		workspaceID, email, role, invitedBy, token, expiresAt, time.Now().UTC(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create invitation: %w", err)
	}

	return &models.WorkspaceInvitation{
		WorkspaceID: workspaceID,
		Email:       email,
		Role:        role,
		InvitedBy:   invitedBy,
		Token:       token,
		ExpiresAt:   expiresAt,
	}, nil
}

func AcceptWorkspaceInvitation(token string, userID uint) error {
	var inv models.WorkspaceInvitation
	err := models.DB.QueryRow(
		"SELECT id, workspace_id, email, role, invited_by, token, expires_at FROM workspace_invitations WHERE token = ? LIMIT 1",
		token,
	).Scan(
		&inv.ID, &inv.WorkspaceID, &inv.Email, &inv.Role, &inv.InvitedBy,
		&inv.Token, &inv.ExpiresAt,
	)
	if err == sql.ErrNoRows {
		return errors.New("invalid invitation token")
	}
	if err != nil {
		return fmt.Errorf("failed to get invitation: %w", err)
	}

	if time.Now().After(inv.ExpiresAt) {
		return errors.New("invitation has expired")
	}

	isMember, err := IsWorkspaceMember(inv.WorkspaceID, userID)
	if err != nil {
		return err
	}
	if isMember {
		return errors.New("user is already a member of this workspace")
	}

	now := time.Now().UTC()
	_, err = models.DB.Exec(
		"INSERT INTO workspace_users (workspace_id, user_id, role, is_active, joined_at, updated_at) VALUES (?, ?, ?, 1, ?, ?)",
		inv.WorkspaceID, userID, inv.Role, now, now,
	)
	if err != nil {
		return fmt.Errorf("failed to add user to workspace: %w", err)
	}

	_, err = models.DB.Exec(
		"UPDATE workspace_invitations SET accepted_at = ? WHERE token = ?", now, token,
	)
	return err
}

func RevokeWorkspaceInvitation(token string) error {
	_, err := models.DB.Exec("DELETE FROM workspace_invitations WHERE token = ?", token)
	if err != nil {
		return fmt.Errorf("failed to revoke invitation: %w", err)
	}
	return nil
}

func GetWorkspaceRole(workspaceID uint, roleName string) (*models.WorkspaceRole, error) {
	var r models.WorkspaceRole
	var permsNull sql.NullString
	err := models.DB.QueryRow(
		"SELECT id, workspace_id, name, description, permissions, is_system, created_at, updated_at FROM workspace_roles WHERE workspace_id = ? AND name = ? LIMIT 1",
		workspaceID, roleName,
	).Scan(
		&r.ID, &r.WorkspaceID, &r.Name, &r.Description, &permsNull, &r.IsSystem,
		&r.CreatedAt, &r.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, errors.New("role not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get workspace role: %w", err)
	}
	if permsNull.Valid {
		r.Permissions = &permsNull.String
	}
	return &r, nil
}

func ListWorkspaceRoles(workspaceID uint) ([]*models.WorkspaceRole, error) {
	rows, err := models.DB.Query(
		"SELECT id, workspace_id, name, description, permissions, is_system, created_at, updated_at FROM workspace_roles WHERE workspace_id = ? ORDER BY is_system DESC, name ASC",
		workspaceID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list workspace roles: %w", err)
	}
	defer rows.Close()

	var roles []*models.WorkspaceRole
	for rows.Next() {
		var r models.WorkspaceRole
		var permsNull sql.NullString
		err := rows.Scan(
			&r.ID, &r.WorkspaceID, &r.Name, &r.Description, &permsNull, &r.IsSystem,
			&r.CreatedAt, &r.UpdatedAt,
		)
		if err != nil {
			continue
		}
		if permsNull.Valid {
			r.Permissions = &permsNull.String
		}
		roles = append(roles, &r)
	}
	return roles, nil
}

func CreateCustomRole(workspaceID uint, roleName, description, permissionsJSON string) (*models.WorkspaceRole, error) {
	now := time.Now().UTC()
	var permArg interface{}
	var permPtr *string
	if permissionsJSON == "" {
		permArg = nil
		permPtr = nil
	} else {
		permArg = permissionsJSON
		permPtr = &permissionsJSON
	}
	result, err := models.DB.Exec(
		"INSERT INTO workspace_roles (workspace_id, name, description, permissions, is_system, created_at, updated_at) VALUES (?, ?, ?, ?, 0, ?, ?)",
		workspaceID, roleName, description, permArg, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create custom role: %w", err)
	}

	id, _ := result.LastInsertId()
	return &models.WorkspaceRole{
		ID:          uint(id),
		WorkspaceID: workspaceID,
		Name:        roleName,
		Description: description,
		Permissions: permPtr,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

func GetWorkspaceRoleByName(workspaceID uint, name string) (*models.WorkspaceRole, error) {
	return GetWorkspaceRole(workspaceID, name)
}

func UpdateCustomRole(roleID uint, name, description, permissionsJSON *string) error {
	var workspaceID uint
	var isSystem bool
	err := models.DB.QueryRow(
		"SELECT workspace_id, is_system FROM workspace_roles WHERE id = ? LIMIT 1", roleID,
	).Scan(&workspaceID, &isSystem)
	if err != nil {
		return errors.New("role not found")
	}
	if isSystem {
		return errors.New("cannot update system role")
	}

	updates := []string{}
	args := []interface{}{}
	if name != nil {
		updates = append(updates, "name = ?")
		args = append(args, *name)
	}
	if description != nil {
		updates = append(updates, "description = ?")
		args = append(args, *description)
	}
	if permissionsJSON != nil {
		updates = append(updates, "permissions = ?")
		if *permissionsJSON == "" {
			args = append(args, nil)
		} else {
			args = append(args, *permissionsJSON)
		}
	}
	if len(updates) == 0 {
		return nil
	}
	updates = append(updates, "updated_at = CURRENT_TIMESTAMP")
	args = append(args, roleID)
	query := "UPDATE workspace_roles SET " + strings.Join(updates, ", ") + " WHERE id = ?"
	_, err = models.DB.Exec(query, args...)
	return err
}

func DeleteCustomRole(roleID uint) error {
	_, err := models.DB.Exec("DELETE FROM workspace_roles WHERE id = ? AND is_system = 0", roleID)
	return err
}

func isDuplicateEntry(err error) bool {
	return err != nil && err.Error() == "Error 1062: Duplicate entry"
}

// ─── Org Admin Workspace Member Management ───────────────────────

// AddWorkspaceMemberByOrgAdmin adds a user to a workspace via org admin authority.
// Validates: workspace belongs to org, caller is org admin, role is member or viewer.
func AddWorkspaceMemberByOrgAdmin(orgID, workspaceID, targetUserID uint, callerID uint, role string) error {
	// Validate workspace belongs to this org
	ws, err := GetWorkspaceByID(workspaceID)
	if err != nil {
		return errors.New("workspace not found")
	}
	if ws.OrganizationID == nil || *ws.OrganizationID != orgID {
		return errors.New("workspace does not belong to the specified organization")
	}

	// Validate caller is org admin
	isAdmin, err := IsOrganizationAdmin(orgID, callerID)
	if err != nil {
		return fmt.Errorf("failed to validate org admin status: %w", err)
	}
	if !isAdmin {
		return errors.New("must be an org admin to add workspace members")
	}

	// Validate role is member or viewer (org admins cannot set owner/admin roles)
	if role == "" {
		role = "member"
	}
	if role != "member" && role != "viewer" {
		return errors.New("org admins can only assign member or viewer roles")
	}

	return AddWorkspaceMember(workspaceID, targetUserID, role)
}

// RemoveWorkspaceMemberByOrgAdmin removes a user from a workspace via org admin authority.
// Validates: workspace belongs to org, caller is org admin, cannot remove owner.
func RemoveWorkspaceMemberByOrgAdmin(orgID, workspaceID, targetUserID uint, callerID uint) error {
	// Validate workspace belongs to this org
	ws, err := GetWorkspaceByID(workspaceID)
	if err != nil {
		return errors.New("workspace not found")
	}
	if ws.OrganizationID == nil || *ws.OrganizationID != orgID {
		return errors.New("workspace does not belong to the specified organization")
	}

	// Validate caller is org admin
	isAdmin, err := IsOrganizationAdmin(orgID, callerID)
	if err != nil {
		return fmt.Errorf("failed to validate org admin status: %w", err)
	}
	if !isAdmin {
		return errors.New("must be an org admin to remove workspace members")
	}

	// Cannot remove workspace owner
	isOwner, err := IsWorkspaceOwner(workspaceID, targetUserID)
	if err != nil {
		return err
	}
	if isOwner {
		return errors.New("cannot remove the workspace owner")
	}

	return RemoveWorkspaceMember(workspaceID, targetUserID)
}

func ListWorkspaceMemberships(userID uint) ([]gin.H, error) {
	rows, err := models.DB.Query(`
		SELECT w.id, w.uuid, w.name, w.slug, wu.role, wu.is_active, wu.joined_at
		FROM workspace_users wu
		INNER JOIN workspaces w ON wu.workspace_id = w.id
		WHERE wu.user_id = ? AND wu.is_active = 1 AND w.is_active = 1
		ORDER BY wu.joined_at DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list workspace memberships: %w", err)
	}
	defer rows.Close()

	var result []gin.H
	for rows.Next() {
		var wu models.WorkspaceUser
		var ws models.Workspace
		err := rows.Scan(&ws.ID, &ws.UUID, &ws.Name, &ws.Slug, &wu.Role, &wu.IsActive, &wu.JoinedAt)
		if err != nil {
			continue
		}
		result = append(result, gin.H{
			"id":    ws.ID,
			"uuid":  ws.UUID,
			"name":  ws.Name,
			"slug":  ws.Slug,
			"role":  wu.Role,
			"active": wu.IsActive,
		})
	}
	return result, nil
}
