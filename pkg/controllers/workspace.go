package controllers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"YourQL/pkg/services"

	"github.com/gin-gonic/gin"
)

func CreateWorkspace(c *gin.Context) {
	var input struct {
		Name           string `json:"name" binding:"required,min=1,max=255"`
		Description    string `json:"description"`
		OrganizationID *uint  `json:"organization_id"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	userIDVal, _ := c.Get("current_workspace_user_id")
	userID := uint(0)
	if u, ok := userIDVal.(uint); ok {
		userID = u
	}

	// Validate org membership if organization_id is provided
	if input.OrganizationID != nil {
		isAdmin, err := services.IsOrganizationAdmin(*input.OrganizationID, userID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to validate organization access"})
			return
		}
		if !isAdmin {
			c.JSON(http.StatusForbidden, gin.H{"error": "You must be an org admin to create workspaces in this organization"})
			return
		}
	} else {
		// No organization specified — personal workspaces are auto-created on email confirmation only.
		c.JSON(http.StatusForbidden, gin.H{"error": "Personal workspaces are automatically created on email confirmation. To create a workspace, join an organization and select it below."})
		return
	}

	workspace, err := services.CreateWorkspace(input.Name, input.Description, userID, input.OrganizationID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.SetCookie("current_workspace_id", workspace.UUID, 3600*24*30, "/", "", false, true)

	c.JSON(http.StatusCreated, gin.H{
		"message": "Workspace created successfully",
		"workspace": gin.H{
			"id":    workspace.ID,
			"uuid":  workspace.UUID,
			"name":  workspace.Name,
			"slug":  workspace.Slug,
		},
	})
}

func ListWorkspaces(c *gin.Context) {
	userIDVal, _ := c.Get("current_workspace_user_id")
	userID := uint(0)
	if u, ok := userIDVal.(uint); ok {
		userID = u
	}

	workspaces, err := services.ListWorkspacesByUser(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var result []gin.H
	for _, w := range workspaces {
		result = append(result, gin.H{
			"id":    w.ID,
			"uuid":  w.UUID,
			"name":  w.Name,
			"slug":  w.Slug,
			"active": w.IsActive,
		})
	}

	c.JSON(http.StatusOK, gin.H{"workspaces": result})
}

func ListWorkspacesGrouped(c *gin.Context) {
	userIDVal, _ := c.Get("current_workspace_user_id")
	userID := uint(0)
	if u, ok := userIDVal.(uint); ok {
		userID = u
	}

	grouped, err := services.ListWorkspacesGroupedByOrg(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"workspaces": grouped})
}

func GetWorkspace(c *gin.Context) {
	workspaceIDStr := c.Param("id")
	workspaceID, err := strconv.ParseUint(workspaceIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid workspace ID"})
		return
	}

	workspace, err := services.GetWorkspaceByID(uint(workspaceID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Workspace not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"workspace": gin.H{
			"id":    workspace.ID,
			"uuid":  workspace.UUID,
			"name":  workspace.Name,
			"slug":  workspace.Slug,
			"active": workspace.IsActive,
		},
	})
}

func UpdateWorkspace(c *gin.Context) {
	workspaceIDStr := c.Param("id")
	workspaceID, err := strconv.ParseUint(workspaceIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid workspace ID"})
		return
	}

	var input struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var name *string
	var desc *string
	if input.Name != "" {
		name = &input.Name
	}
	if input.Description != "" {
		desc = &input.Description
	}

	workspace, err := services.UpdateWorkspace(uint(workspaceID), name, desc)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Workspace updated successfully",
		"workspace": gin.H{
			"id":    workspace.ID,
			"uuid":  workspace.UUID,
			"name":  workspace.Name,
			"slug":  workspace.Slug,
		},
	})
}

func DeleteWorkspace(c *gin.Context) {
	workspaceIDStr := c.Param("id")
	workspaceID, err := strconv.ParseUint(workspaceIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid workspace ID"})
		return
	}

	// Check if this is a personal workspace
	userIDVal, _ := c.Get("current_workspace_user_id")
	userID := uint(0)
	if u, ok := userIDVal.(uint); ok {
		userID = u
	}

	isPersonal, err := services.IsPersonalWorkspace(uint(workspaceID), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}
	if isPersonal {
		c.JSON(http.StatusForbidden, gin.H{"error": "Cannot delete personal workspace"})
		return
	}

	err = services.DeleteWorkspace(uint(workspaceID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Workspace deleted successfully"})
}

func SwitchWorkspace(c *gin.Context) {
	workspaceIDStr := c.Param("id")
	workspaceID, err := strconv.ParseUint(workspaceIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid workspace ID"})
		return
	}

	userIDVal, _ := c.Get("current_workspace_user_id")
	userID := uint(0)
	if u, ok := userIDVal.(uint); ok {
		userID = u
	}

	workspace, err := services.GetWorkspaceByID(uint(workspaceID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Workspace not found"})
		return
	}

	isMember, err := services.IsWorkspaceMember(uint(workspaceID), userID)
	if err != nil || !isMember {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	c.SetCookie("current_workspace_id", workspace.UUID, 3600*24*30, "/", "", false, true)

	c.JSON(http.StatusOK, gin.H{
		"message": "Workspace switched successfully",
		"workspace": gin.H{
			"id":   workspace.ID,
			"uuid": workspace.UUID,
			"name": workspace.Name,
		},
	})
}

func GetWorkspaceMember(c *gin.Context) {
	workspaceIDStr := c.Param("id")
	workspaceID, err := strconv.ParseUint(workspaceIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid workspace ID"})
		return
	}

	userIDStr := c.Param("uid")
	userID, err := strconv.ParseUint(userIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
		return
	}

	member, err := services.GetWorkspaceMember(uint(workspaceID), uint(userID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Member not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"member": gin.H{
			"id":         member.ID,
			"user_id":    member.UserID,
			"role":       member.Role,
			"active":     member.IsActive,
			"joined_at":  member.JoinedAt,
		},
	})
}

func AddWorkspaceMember(c *gin.Context) {
	workspaceIDStr := c.Param("id")
	workspaceID, err := strconv.ParseUint(workspaceIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid workspace ID"})
		return
	}

	var input struct {
		UserID uint   `json:"user_id" binding:"required"`
		Role   string `json:"role"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	err = services.AddWorkspaceMember(uint(workspaceID), input.UserID, input.Role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Member added successfully"})
}

func RemoveWorkspaceMember(c *gin.Context) {
	workspaceIDStr := c.Param("id")
	workspaceID, err := strconv.ParseUint(workspaceIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid workspace ID"})
		return
	}

	userIDStr := c.Param("uid")
	userID, err := strconv.ParseUint(userIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
		return
	}

	err = services.RemoveWorkspaceMember(uint(workspaceID), uint(userID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Member removed successfully"})
}

func UpdateMemberRole(c *gin.Context) {
	workspaceIDStr := c.Param("id")
	workspaceID, err := strconv.ParseUint(workspaceIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid workspace ID"})
		return
	}

	userIDStr := c.Param("uid")
	userID, err := strconv.ParseUint(userIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
		return
	}

	var input struct {
		Role string `json:"role" binding:"required"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	err = services.UpdateWorkspaceMemberRole(uint(workspaceID), uint(userID), input.Role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Role updated successfully"})
}

func ListWorkspaceMembers(c *gin.Context) {
	workspaceIDStr := c.Param("id")
	workspaceID, err := strconv.ParseUint(workspaceIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid workspace ID"})
		return
	}

	members, err := services.ListWorkspaceMembers(uint(workspaceID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var result []gin.H
	for _, m := range members {
		result = append(result, gin.H{
			"id":         m.ID,
			"user_id":    m.UserID,
			"role":       m.Role,
			"active":     m.IsActive,
			"joined_at":  m.JoinedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{"members": result})
}

func CreateInvitation(c *gin.Context) {
	workspaceIDStr := c.Param("id")
	workspaceID, err := strconv.ParseUint(workspaceIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid workspace ID"})
		return
	}

	var input struct {
		Email string `json:"email" binding:"required,email"`
		Role  string `json:"role"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	userIDVal, _ := c.Get("current_workspace_user_id")
	userID := uint(0)
	if u, ok := userIDVal.(uint); ok {
		userID = u
	}

	inv, err := services.CreateWorkspaceInvitation(uint(workspaceID), input.Email, input.Role, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Invitation sent successfully",
		"invitation": gin.H{
			"email": inv.Email,
			"role":  inv.Role,
			"token": inv.Token,
		},
	})
}

func GetWorkspaceRole(c *gin.Context) {
	workspaceIDStr := c.Param("id")
	workspaceID, err := strconv.ParseUint(workspaceIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid workspace ID"})
		return
	}

	roleName := c.Param("role")
	role, err := services.GetWorkspaceRole(uint(workspaceID), roleName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Role not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"role": gin.H{
			"id":          role.ID,
			"name":        role.Name,
			"description": role.Description,
			"system":      role.IsSystem,
		},
	})
}

func ListWorkspaceRoles(c *gin.Context) {
	workspaceIDStr := c.Param("id")
	workspaceID, err := strconv.ParseUint(workspaceIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid workspace ID"})
		return
	}

	roles, err := services.ListWorkspaceRoles(uint(workspaceID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var result []gin.H
	for _, r := range roles {
		result = append(result, gin.H{
			"id":          r.ID,
			"name":        r.Name,
			"description": r.Description,
			"system":      r.IsSystem,
		})
	}

	c.JSON(http.StatusOK, gin.H{"roles": result})
}

func CreateCustomRole(c *gin.Context) {
	workspaceIDStr := c.Param("id")
	workspaceID, err := strconv.ParseUint(workspaceIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid workspace ID"})
		return
	}

	var input struct {
		Name        string `json:"name" binding:"required"`
		Description string `json:"description"`
		Permissions string `json:"permissions" binding:"required"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if !isValidJSON(input.Permissions) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Permissions must be valid JSON"})
		return
	}

	role, err := services.CreateCustomRole(uint(workspaceID), input.Name, input.Description, input.Permissions)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "Role created successfully",
		"role": gin.H{
			"id":          role.ID,
			"name":        role.Name,
			"description": role.Description,
		},
	})
}

func UpdateCustomRole(c *gin.Context) {
	roleIDStr := c.Param("rid")
	roleID, err := strconv.ParseUint(roleIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid role ID"})
		return
	}

	var input struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		Permissions *string `json:"permissions"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	err = services.UpdateCustomRole(uint(roleID), input.Name, input.Description, input.Permissions)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Role updated successfully"})
}

func DeleteCustomRole(c *gin.Context) {
	roleIDStr := c.Param("rid")
	roleID, err := strconv.ParseUint(roleIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid role ID"})
		return
	}

	err = services.DeleteCustomRole(uint(roleID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Role deleted successfully"})
}

func isValidJSON(s string) bool {
	var js interface{}
	return json.Unmarshal([]byte(s), &js) == nil
}
