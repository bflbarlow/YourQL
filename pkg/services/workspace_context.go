package services

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"YourQL/pkg/models"
)

// GetWorkspaceFromUserContext retrieves the current user's active workspace.
func GetWorkspaceFromUserContext(workspaceID uint, userID uint) (*models.Workspace, error) {
	isMember, err := IsWorkspaceMember(workspaceID, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to verify workspace membership: %w", err)
	}
	if !isMember {
		return nil, errors.New("user is not a member of this workspace")
	}
	return GetWorkspaceByID(workspaceID)
}

// GetWorkspaceFromRequest extracts the workspace ID from various sources.
func GetWorkspaceFromRequest(workspaceID string, userID uint) (*models.Workspace, error) {
	if workspaceID == "" {
		return nil, errors.New("workspace ID is required")
	}

	workspace, err := GetWorkspaceByUUID(workspaceID)
	if err == nil {
		return workspace, nil
	}

	workspace, err = GetWorkspaceBySlug(workspaceID)
	if err == nil {
		return workspace, nil
	}

	return nil, fmt.Errorf("workspace not found: %s", workspaceID)
}

// GetUserWorkspaceRole returns the user's role in a specific workspace.
func GetUserWorkspaceRole(workspaceID, userID uint) (string, error) {
	var role string
	err := models.DB.QueryRow(
		"SELECT role FROM workspace_users WHERE workspace_id = ? AND user_id = ? AND is_active = 1 LIMIT 1",
		workspaceID, userID,
	).Scan(&role)
	if err == sql.ErrNoRows {
		return "", errors.New("user is not a member of this workspace")
	}
	if err != nil {
		return "", fmt.Errorf("failed to get workspace role: %w", err)
	}
	return role, nil
}

// GetUserWorkspaceRoles returns all roles assigned to a user in a workspace.
func GetUserWorkspaceRoles(workspaceID, userID uint) ([]*models.WorkspaceUserRole, error) {
	rows, err := models.DB.Query(`
		SELECT wur.id, wur.workspace_user_id, wur.workspace_role_id, wur.assigned_by, wur.assigned_at
		FROM workspace_user_roles wur
		INNER JOIN workspace_users wu ON wur.workspace_user_id = wu.id
		WHERE wu.workspace_id = ? AND wu.user_id = ?
	`, workspaceID, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get workspace user roles: %w", err)
	}
	defer rows.Close()

	var roles []*models.WorkspaceUserRole
	for rows.Next() {
		var r models.WorkspaceUserRole
		err := rows.Scan(&r.ID, &r.WorkspaceUserID, &r.WorkspaceRoleID, &r.AssignedBy, &r.AssignedAt)
		if err != nil {
			continue
		}
		roles = append(roles, &r)
	}
	return roles, nil
}

// AssignRoleToUser assigns a role to a user in a workspace.
func AssignRoleToUser(workspaceUserID, workspaceRoleID uint, assignedBy uint) error {
	_, err := models.DB.Exec(
		"INSERT INTO workspace_user_roles (workspace_user_id, workspace_role_id, assigned_by) VALUES (?, ?, ?)",
		workspaceUserID, workspaceRoleID, assignedBy,
	)
	if err != nil {
		if isDuplicateEntry(err) {
			return errors.New("role assignment already exists")
		}
		return fmt.Errorf("failed to assign role: %w", err)
	}
	return nil
}

// RemoveRoleFromUser removes a role assignment from a user in a workspace.
func RemoveRoleFromUser(workspaceUserID, workspaceRoleID uint) error {
	_, err := models.DB.Exec(
		"DELETE FROM workspace_user_roles WHERE workspace_user_id = ? AND workspace_role_id = ?",
		workspaceUserID, workspaceRoleID,
	)
	if err != nil {
		return fmt.Errorf("failed to remove role: %w", err)
	}
	return nil
}

// CheckPermission checks if a user's role in a workspace grants a specific permission.
func CheckPermission(workspaceID, userID uint, permission string) (bool, error) {
	role, err := GetUserWorkspaceRole(workspaceID, userID)
	if err != nil {
		return false, err
	}

	switch role {
	case "owner":
		return true, nil
	case "admin":
		return checkAdminPermission(permission), nil
	case "member":
		return checkMemberPermission(permission), nil
	case "viewer":
		return checkViewerPermission(permission), nil
	}

	roles, err := ListWorkspaceRoles(workspaceID)
	if err != nil {
		return false, err
	}

	for _, r := range roles {
		if r.Name == role && r.Permissions != nil {
			return checkPermissionFromJSON(*r.Permissions, permission)
		}
	}

	return false, nil
}

func checkAdminPermission(permission string) bool {
	adminPerms := map[string]bool{
		"can_query":                true,
		"can_manage_llm":           true,
		"can_manage_db":            true,
		"can_manage_members":       true,
		"can_manage_roles":         false,
		"can_manage_settings":      true,
		"can_save_queries":         true,
		"can_view_history":         true,
		"can_create_conversations": true,
		"can_export_data":          true,
	}
	return adminPerms[permission]
}

func checkMemberPermission(permission string) bool {
	memberPerms := map[string]bool{
		"can_query":                true,
		"can_manage_llm":           false,
		"can_manage_db":            false,
		"can_manage_members":       false,
		"can_manage_roles":         false,
		"can_manage_settings":      false,
		"can_save_queries":         true,
		"can_view_history":         true,
		"can_create_conversations": true,
		"can_export_data":          false,
	}
	return memberPerms[permission]
}

func checkViewerPermission(permission string) bool {
	viewerPerms := map[string]bool{
		"can_query":                false,
		"can_manage_llm":           false,
		"can_manage_db":            false,
		"can_manage_members":       false,
		"can_manage_roles":         false,
		"can_manage_settings":      false,
		"can_save_queries":         false,
		"can_view_history":         true,
		"can_create_conversations": false,
		"can_export_data":          false,
	}
	return viewerPerms[permission]
}

func checkPermissionFromJSON(permissionsJSON, permission string) (bool, error) {
	var perms map[string]interface{}
	if err := json.Unmarshal([]byte(permissionsJSON), &perms); err != nil {
		return false, fmt.Errorf("invalid permissions JSON: %w", err)
	}
	val, ok := perms[permission]
	if !ok {
		return false, nil
	}
	switch v := val.(type) {
	case bool:
		return v, nil
	case string:
		return v == "true" || v == "1", nil
	default:
		return false, nil
	}
}

// CheckOrgPermission checks if a user has a specific permission within an organization.
// Org roles map to permissions as follows:
//   - owner: full access (all permissions true)
//   - admin: full workspace management, but no org deletion
//   - member: read-only within org scope
func CheckOrgPermission(orgID, userID uint, permission string) (bool, error) {
	// Check if user is an org member
	isMember, err := IsOrganizationMember(orgID, userID)
	if err != nil {
		return false, err
	}
	if !isMember {
		return false, nil
	}

	// Get user's org role
	orgRole, err := GetOrganizationMemberRole(orgID, userID)
	if err != nil {
		return false, err
	}

	// Owner has full org permissions
	if orgRole == "owner" {
		return true, nil
	}

	// Admin has full workspace management + member management
	if orgRole == "admin" {
		adminPerms := map[string]bool{
			"can_manage_workspace_members": true,
			"can_create_workspace":         true,
			"can_delete_workspace":         true,
			"can_manage_members":           true,
			"can_manage_settings":          true,
		}
		if adminPerms[permission] {
			return true, nil
		}
		return false, nil
	}

	// Member has read-only org-level access
	return false, nil
}

// CanOrgAdminManageWorkspaceMember checks if an org admin can manage a workspace member.
// Returns true if the caller is an org admin/owner AND the workspace belongs to that org.
func CanOrgAdminManageWorkspaceMember(orgID, callerID, workspaceID uint) (bool, error) {
	// Check caller is org admin/owner
	isAdmin, err := IsOrganizationAdmin(orgID, callerID)
	if err != nil {
		return false, err
	}
	if !isAdmin {
		return false, nil
	}

	// Check workspace belongs to this org
	ws, err := GetWorkspaceByID(workspaceID)
	if err != nil {
		return false, nil
	}
	if ws.OrganizationID == nil || *ws.OrganizationID != orgID {
		return false, nil
	}

	return true, nil
}

// GetEffectiveWorkspacePermissions returns the effective permissions for a user
// in a workspace, considering both org-level and workspace-level roles.
func GetEffectiveWorkspacePermissions(workspaceID, userID uint) (map[string]bool, error) {
	// Start with workspace-level role
	wsRole, err := GetUserWorkspaceRole(workspaceID, userID)
	if err != nil {
		// Fallback to minimal permissions
		return map[string]bool{
			"can_query":                false,
			"can_manage_llm":           false,
			"can_manage_db":            false,
			"can_manage_members":       false,
			"can_manage_roles":         false,
			"can_manage_settings":      false,
			"can_save_queries":         false,
			"can_view_history":         false,
			"can_create_conversations": false,
			"can_export_data":          false,
		}, nil
	}

	// Determine base permissions from workspace role
	var perms map[string]bool
	switch wsRole {
	case "owner":
		perms = map[string]bool{
			"can_query":                true,
			"can_manage_llm":           true,
			"can_manage_db":            true,
			"can_manage_members":       true,
			"can_manage_roles":         true,
			"can_manage_settings":      true,
			"can_save_queries":         true,
			"can_view_history":         true,
			"can_create_conversations": true,
			"can_export_data":          true,
		}
	case "admin":
		perms = map[string]bool{
			"can_query":                true,
			"can_manage_llm":           true,
			"can_manage_db":            true,
			"can_manage_members":       true,
			"can_manage_roles":         false,
			"can_manage_settings":      true,
			"can_save_queries":         true,
			"can_view_history":         true,
			"can_create_conversations": true,
			"can_export_data":          true,
		}
	case "member":
		perms = map[string]bool{
			"can_query":                true,
			"can_manage_llm":           false,
			"can_manage_db":            false,
			"can_manage_members":       false,
			"can_manage_roles":         false,
			"can_manage_settings":      false,
			"can_save_queries":         true,
			"can_view_history":         true,
			"can_create_conversations": true,
			"can_export_data":          false,
		}
	case "viewer":
		perms = map[string]bool{
			"can_query":                false,
			"can_manage_llm":           false,
			"can_manage_db":            false,
			"can_manage_members":       false,
			"can_manage_roles":         false,
			"can_manage_settings":      false,
			"can_save_queries":         false,
			"can_view_history":         true,
			"can_create_conversations": false,
			"can_export_data":          false,
		}
	default:
		perms = map[string]bool{}
	}

	// Check if user is org admin/owner — if so, grant full workspace management
	ws, err := GetWorkspaceByID(workspaceID)
	if err == nil && ws.OrganizationID != nil {
		isOrgAdmin, _ := IsOrganizationAdmin(*ws.OrganizationID, userID)
		if isOrgAdmin {
			// Org admins get full workspace management permissions
			perms["can_manage_members"] = true
			perms["can_manage_settings"] = true
			perms["can_manage_llm"] = true
			perms["can_manage_db"] = true
		}
	}

	return perms, nil
}
