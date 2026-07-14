package controllers

import (
	"net/http"
	"strconv"

	"YourQL/pkg/environment"
	"YourQL/pkg/models"
	"YourQL/pkg/services"
	"YourQL/pkg/utils"

	"github.com/gin-gonic/gin"
)

// createOrganizationHandler handles POST /api/admin/organizations
func CreateOrganizationHandler(c *gin.Context) {
	// Feature flag check
	if environment.EnableOrgFeatures() != "true" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization features are not enabled"})
		return
	}

	// Admin check
	isAdmin := checkIsAdmin(c)
	if !isAdmin {
		return
	}

	var input struct {
		Name             string `json:"name" binding:"required,min=1,max=100"`
		Slug             string `json:"slug"`
		FirstAdminEmail  string `json:"first_admin_email" binding:"required,email"`
		FirstAdminRole   string `json:"first_admin_role"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate org role
	if input.FirstAdminRole == "" {
		input.FirstAdminRole = "admin"
	}
	if !models.IsValidOrgRole(input.FirstAdminRole) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid role: must be owner, admin, or member"})
		return
	}

	// Sanitize inputs
	sanitizedName := utils.SanitizeEmail(input.Name) // reuse email sanitizer for basic sanitization
	if sanitizedName == "" {
		sanitizedName = input.Name
	}
	if sanitizedName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid organization name"})
		return
	}

	firstAdminEmail := utils.SanitizeEmail(input.FirstAdminEmail)

	// Get admin user info
	userIDVal, _ := c.Get("user_id")
	var adminUserID float64
	if v, ok := userIDVal.(float64); ok {
		adminUserID = v
	}

	// Create organization
	org, err := services.CreateOrganization(sanitizedName, input.Slug, uint(adminUserID), firstAdminEmail, input.FirstAdminRole)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "Organization created successfully",
		"organization": gin.H{
			"id":   org.ID,
			"name": org.Name,
			"slug": org.Slug,
		},
	})
}

// getOrganizationHandler handles GET /api/organizations/:id
func GetOrganizationHandler(c *gin.Context) {
	if environment.EnableOrgFeatures() != "true" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization features are not enabled"})
		return
	}

	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid organization ID"})
		return
	}

	org, err := services.GetOrganizationByID(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Organization not found"})
		return
	}

	// Check membership
	userID := getUintFromContext(c, "user_id")
	isMember, _ := services.IsOrganizationMember(uint(id), userID)
	if !isMember {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	role, _ := services.GetOrganizationMemberRole(uint(id), userID)
	memberCount, _ := services.GetOrganizationMemberCount(uint(id))

	c.JSON(http.StatusOK, gin.H{
		"organization": gin.H{
			"id":           org.ID,
			"name":         org.Name,
			"slug":         org.Slug,
			"is_active":    org.IsActive,
			"created_at":   org.CreatedAt,
			"updated_at":   org.UpdatedAt,
		},
		"user_role": role,
		"member_count": memberCount,
	})
}

// listOrganizationsHandler handles GET /api/organizations
func ListOrganizationsHandler(c *gin.Context) {
	if environment.EnableOrgFeatures() != "true" {
		c.JSON(http.StatusOK, gin.H{"organizations": []interface{}{}})
		return
	}

	userID := getUintFromContext(c, "user_id")
	if userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication required"})
		return
	}

	orgs, err := services.ListOrganizationsByUser(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var result []gin.H
	for _, org := range orgs {
		role, _ := services.GetOrganizationMemberRole(org.ID, userID)
		memberCount, _ := services.GetOrganizationMemberCount(org.ID)
		result = append(result, gin.H{
			"id":           org.ID,
			"name":         org.Name,
			"slug":         org.Slug,
			"is_active":    org.IsActive,
			"user_role":    role,
			"member_count": memberCount,
		})
	}

	c.JSON(http.StatusOK, gin.H{"organizations": result})
}

// addMemberHandler handles POST /api/organizations/:id/members
func AddMemberHandler(c *gin.Context) {
	if environment.EnableOrgFeatures() != "true" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization features are not enabled"})
		return
	}

	idStr := c.Param("id")
	orgID, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid organization ID"})
		return
	}

	userID := getUintFromContext(c, "user_id")
	isAdmin, _ := services.IsOrganizationAdmin(uint(orgID), userID)
	isOwner, _ := services.IsOrganizationOwner(uint(orgID), userID)
	if !isAdmin && !isOwner {
		c.JSON(http.StatusForbidden, gin.H{"error": "Only org owners and admins can add members"})
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

	if input.Role == "" {
		input.Role = "member"
	}
	if !models.IsValidOrgRole(input.Role) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid role: must be owner, admin, or member"})
		return
	}

	_, err = services.AddOrganizationMember(uint(orgID), input.UserID, input.Role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Member added successfully"})
}

// removeMemberHandler handles DELETE /api/organizations/:id/members/:uid
func RemoveMemberHandler(c *gin.Context) {
	if environment.EnableOrgFeatures() != "true" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization features are not enabled"})
		return
	}

	idStr := c.Param("id")
	orgID, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid organization ID"})
		return
	}

	uidStr := c.Param("uid")
	userID, err := strconv.ParseUint(uidStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
		return
	}

	currentUserID := getUintFromContext(c, "user_id")
	isAdmin, _ := services.IsOrganizationAdmin(uint(orgID), currentUserID)
	isOwner, _ := services.IsOrganizationOwner(uint(orgID), currentUserID)
	if !isAdmin && !isOwner {
		c.JSON(http.StatusForbidden, gin.H{"error": "Only org owners and admins can remove members"})
		return
	}

	// Prevent removing the last owner
	if currentUserID != uint(userID) {
		isMemberOwner, _ := services.IsOrganizationOwner(uint(orgID), uint(userID))
		if isMemberOwner {
			// Count other owners (excluding the one being removed)
			var ownerCount int
			err = models.DB.QueryRow(
				"SELECT COUNT(*) FROM organization_members WHERE organization_id = ? AND role = 'owner' AND is_active = 1 AND user_id != ?",
				uint(orgID), uint(userID),
			).Scan(&ownerCount)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			if ownerCount <= 1 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Cannot remove the last owner. Transfer ownership first."})
				return
			}
		}
	}

	err = services.RemoveOrganizationMember(uint(orgID), uint(userID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Member removed successfully"})
}

// updateMemberRoleHandler handles PUT /api/organizations/:id/members/:uid/role
func UpdateMemberRoleHandler(c *gin.Context) {
	if environment.EnableOrgFeatures() != "true" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization features are not enabled"})
		return
	}

	idStr := c.Param("id")
	orgID, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid organization ID"})
		return
	}

	uidStr := c.Param("uid")
	userID, err := strconv.ParseUint(uidStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
		return
	}

	currentUserID := getUintFromContext(c, "user_id")
	isAdmin, _ := services.IsOrganizationAdmin(uint(orgID), currentUserID)
	isOwner, _ := services.IsOrganizationOwner(uint(orgID), currentUserID)
	if !isAdmin && !isOwner {
		c.JSON(http.StatusForbidden, gin.H{"error": "Only org owners and admins can update roles"})
		return
	}

	var input struct {
		Role string `json:"role" binding:"required"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if !models.IsValidOrgRole(input.Role) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid role: must be owner, admin, or member"})
		return
	}

	_, err = services.UpdateOrganizationMemberRole(uint(orgID), uint(userID), input.Role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Role updated successfully"})
}

// transferOwnershipHandler handles POST /api/organizations/:id/transfer
func TransferOwnershipHandler(c *gin.Context) {
	if environment.EnableOrgFeatures() != "true" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization features are not enabled"})
		return
	}

	idStr := c.Param("id")
	orgID, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid organization ID"})
		return
	}

	currentUserID := getUintFromContext(c, "user_id")
	isOwner, _ := services.IsOrganizationOwner(uint(orgID), currentUserID)
	if !isOwner {
		c.JSON(http.StatusForbidden, gin.H{"error": "Only org owners can transfer ownership"})
		return
	}

	var input struct {
		NewOwnerID uint `json:"new_owner_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	err = services.TransferOrganizationOwnership(uint(orgID), input.NewOwnerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Ownership transferred successfully"})
}

// leaveOrganizationHandler handles POST /api/me/organizations/:id/leave
func LeaveOrganizationHandler(c *gin.Context) {
	if environment.EnableOrgFeatures() != "true" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization features are not enabled"})
		return
	}

	idStr := c.Param("id")
	orgID, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid organization ID"})
		return
	}

	currentUserID := getUintFromContext(c, "user_id")

	err = services.LeaveOrganization(uint(orgID), currentUserID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Left organization successfully"})
}

// deleteOrganizationHandler handles DELETE /api/organizations/:id
func DeleteOrganizationHandler(c *gin.Context) {
	if environment.EnableOrgFeatures() != "true" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization features are not enabled"})
		return
	}

	idStr := c.Param("id")
	orgID, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid organization ID"})
		return
	}

	currentUserID := getUintFromContext(c, "user_id")
	isOwner, _ := services.IsOrganizationOwner(uint(orgID), currentUserID)
	if !isOwner {
		c.JSON(http.StatusForbidden, gin.H{"error": "Only org owners can delete the organization"})
		return
	}

	err = services.DeleteOrganization(uint(orgID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Organization deleted successfully"})
}

// ─── Org Admin Workspace Member Management ───────────────────────

// addWorkspaceMemberHandler handles POST /api/organizations/:id/workspaces/:wsid/members
func AddWorkspaceMemberHandler(c *gin.Context) {
	if environment.EnableOrgFeatures() != "true" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization features are not enabled"})
		return
	}

	idStr := c.Param("id")
	orgID, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid organization ID"})
		return
	}

	wsidStr := c.Param("wsid")
	workspaceID, err := strconv.ParseUint(wsidStr, 10, 32)
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

	// Validate caller is org admin
	callerID := getUintFromContext(c, "user_id")
	if callerID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication required"})
		return
	}

	err = services.AddWorkspaceMemberByOrgAdmin(uint(orgID), uint(workspaceID), input.UserID, callerID, input.Role)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Member added to workspace successfully"})
}

// removeWorkspaceMemberHandler handles DELETE /api/organizations/:id/workspaces/:wsid/members/:uid
func RemoveWorkspaceMemberHandler(c *gin.Context) {
	if environment.EnableOrgFeatures() != "true" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Organization features are not enabled"})
		return
	}

	idStr := c.Param("id")
	orgID, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid organization ID"})
		return
	}

	wsidStr := c.Param("wsid")
	workspaceID, err := strconv.ParseUint(wsidStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid workspace ID"})
		return
	}

	uidStr := c.Param("uid")
	userID, err := strconv.ParseUint(uidStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user ID"})
		return
	}

	// Validate caller is org admin
	callerID := getUintFromContext(c, "user_id")
	if callerID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication required"})
		return
	}

	_ = getUintFromContext(c, "user_id")

	err = services.RemoveWorkspaceMemberByOrgAdmin(uint(orgID), uint(workspaceID), uint(userID), callerID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Member removed from workspace successfully"})
}

// ─── Helpers ───────────────────────────────────────────────────────

func checkIsAdmin(c *gin.Context) bool {
	// Try to get user_id from context first
	userIDVal, exists := c.Get("user_id")
	if !exists {
		// Try to extract from cookie
		cookie, err := c.Cookie("token")
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "No token found"})
			return false
		}
		claims, err := services.ValidateJWT(cookie)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired token"})
			return false
		}
		userIDVal = (*claims)["user_id"]
		c.Set("user_id", userIDVal)
	}

	// Check is_admin flag on the users table
	var isAdmin int
	err := models.DB.QueryRow(
		"SELECT is_admin FROM users WHERE id = ? LIMIT 1",
		userIDVal,
	).Scan(&isAdmin)
	if err != nil || isAdmin == 0 {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin access required"})
		return false
	}

	return true
}
