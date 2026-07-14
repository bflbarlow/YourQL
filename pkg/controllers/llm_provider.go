package controllers

import (
	"net/http"

	"YourQL/pkg/models"
	"YourQL/pkg/services"

	"github.com/gin-gonic/gin"
)

// ListLLMProviders lists all LLM providers for the current workspace.
func ListLLMProviders(c *gin.Context) {
	workspaceVal, exists := c.Get("current_workspace")
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "No workspace selected"})
		return
	}
	workspace, ok := workspaceVal.(*models.Workspace)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Invalid workspace"})
		return
	}

	userID := getUintFromContext(c, "user_id")
	if userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication required"})
		return
	}

	isMember, err := services.IsWorkspaceMember(workspace.ID, userID)
	if err != nil || !isMember {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	providers, err := services.ListLLMProvidersByWorkspace(workspace.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load LLM providers"})
		return
	}

	type providerSummary struct {
		ID          uint   `json:"id"`
		Name        string `json:"name"`
		Provider    string `json:"provider"`
		Model       string `json:"model"`
		BaseURL     string `json:"base_url"`
		IsDefault   bool   `json:"is_default"`
		IsActive    bool   `json:"is_active"`
		CreatedAt   string `json:"created_at"`
		UpdatedAt   string `json:"updated_at"`
	}

	var summaries []providerSummary
	for _, p := range providers {
		model := ""
		if p.Model != nil {
			model = *p.Model
		}
		baseURL := ""
		if p.BaseURL != nil {
			baseURL = *p.BaseURL
		}
		summaries = append(summaries, providerSummary{
			ID:        p.ID,
			Name:      p.Name,
			Provider:  p.Provider,
			Model:     model,
			BaseURL:   baseURL,
			IsDefault: p.IsDefault,
			IsActive:  p.IsActive,
			CreatedAt: p.CreatedAt.Format("Jan 2, 2006"),
			UpdatedAt: p.UpdatedAt.Format("Jan 2, 2006"),
		})
	}

	c.JSON(http.StatusOK, gin.H{"providers": summaries})
}

// GetLLMProvider retrieves a single LLM provider by ID.
func GetLLMProvider(c *gin.Context) {
	id := getUintFromParam(c, "id")
	if id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid LLM provider ID"})
		return
	}

	p, err := services.GetLLMProviderByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "LLM provider not found"})
		return
	}

	workspaceVal, exists := c.Get("current_workspace")
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "No workspace selected"})
		return
	}
	workspace, ok := workspaceVal.(*models.Workspace)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Invalid workspace"})
		return
	}

	userID := getUintFromContext(c, "user_id")
	isMember, _ := services.IsWorkspaceMember(workspace.ID, userID)
	if !isMember {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	model := ""
	if p.Model != nil {
		model = *p.Model
	}
	baseURL := ""
	if p.BaseURL != nil {
		baseURL = *p.BaseURL
	}

	c.JSON(http.StatusOK, gin.H{
		"provider": gin.H{
			"id":         p.ID,
			"name":       p.Name,
			"provider":   p.Provider,
			"model":      model,
			"base_url":   baseURL,
			"is_default": p.IsDefault,
			"is_active":  p.IsActive,
			"created_at": p.CreatedAt.Format("Jan 2, 2006"),
			"updated_at": p.UpdatedAt.Format("Jan 2, 2006"),
		},
	})
}

// CreateLLMProvider creates a new LLM provider.
func CreateLLMProvider(c *gin.Context) {
	workspaceVal, exists := c.Get("current_workspace")
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "No workspace selected"})
		return
	}
	workspace, ok := workspaceVal.(*models.Workspace)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Invalid workspace"})
		return
	}

	userID := getUintFromContext(c, "user_id")
	if userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication required"})
		return
	}

	isMember, err := services.IsWorkspaceMember(workspace.ID, userID)
	if err != nil || !isMember {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	hasPermission, err := services.CheckPermission(workspace.ID, userID, "can_manage_llm")
	if err != nil || !hasPermission {
		c.JSON(http.StatusForbidden, gin.H{"error": "Insufficient permissions"})
		return
	}

	var input struct {
		Name      string `json:"name" binding:"required"`
		Provider  string `json:"provider" binding:"required"`
		Model     string `json:"model"`
		BaseURL   string `json:"base_url"`
		APIKey    string `json:"api_key"`
		IsDefault bool   `json:"is_default"`
		Config    string `json:"config"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input"})
		return
	}

	provider, err := services.CreateLLMProvider(workspace.ID, userID, input.Name, input.Provider, input.Model, input.BaseURL, input.APIKey, input.IsDefault, input.Config)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"provider": provider})
}

// UpdateLLMProvider updates an LLM provider.
func UpdateLLMProvider(c *gin.Context) {
	id := getUintFromParam(c, "id")
	if id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid LLM provider ID"})
		return
	}

	_, err := services.GetLLMProviderByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "LLM provider not found"})
		return
	}

	workspaceVal, exists := c.Get("current_workspace")
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "No workspace selected"})
		return
	}
	workspace, ok := workspaceVal.(*models.Workspace)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Invalid workspace"})
		return
	}

	userID := getUintFromContext(c, "user_id")
	isMember, _ := services.IsWorkspaceMember(workspace.ID, userID)
	if !isMember {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	hasPermission, err := services.CheckPermission(workspace.ID, userID, "can_manage_llm")
	if err != nil || !hasPermission {
		c.JSON(http.StatusForbidden, gin.H{"error": "Insufficient permissions"})
		return
	}

	var input struct {
		Name      *string `json:"name"`
		Model     *string `json:"model"`
		BaseURL   *string `json:"base_url"`
		APIKey    *string `json:"api_key"`
		Config    *string `json:"config"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input"})
		return
	}

	updated, err := services.UpdateLLMProvider(id, input.Name, input.Model, input.BaseURL, input.APIKey, input.Config)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"provider": updated})
}

// DeleteLLMProvider deletes an LLM provider.
func DeleteLLMProvider(c *gin.Context) {
	id := getUintFromParam(c, "id")
	if id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid LLM provider ID"})
		return
	}

	_, err := services.GetLLMProviderByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "LLM provider not found"})
		return
	}

	workspaceVal, exists := c.Get("current_workspace")
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "No workspace selected"})
		return
	}
	workspace, ok := workspaceVal.(*models.Workspace)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Invalid workspace"})
		return
	}

	userID := getUintFromContext(c, "user_id")
	isMember, _ := services.IsWorkspaceMember(workspace.ID, userID)
	if !isMember {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	hasPermission, err := services.CheckPermission(workspace.ID, userID, "can_manage_llm")
	if err != nil || !hasPermission {
		c.JSON(http.StatusForbidden, gin.H{"error": "Insufficient permissions"})
		return
	}

	if err := services.DeleteLLMProvider(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "LLM provider deleted"})
}

// SetDefaultLLMProvider sets an LLM provider as the default.
func SetDefaultLLMProvider(c *gin.Context) {
	id := getUintFromParam(c, "id")
	if id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid LLM provider ID"})
		return
	}

	workspaceVal, exists := c.Get("current_workspace")
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "No workspace selected"})
		return
	}
	workspace, ok := workspaceVal.(*models.Workspace)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Invalid workspace"})
		return
	}

	userID := getUintFromContext(c, "user_id")
	isMember, _ := services.IsWorkspaceMember(workspace.ID, userID)
	if !isMember {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	hasPermission, err := services.CheckPermission(workspace.ID, userID, "can_manage_llm")
	if err != nil || !hasPermission {
		c.JSON(http.StatusForbidden, gin.H{"error": "Insufficient permissions"})
		return
	}

	if err := services.SetDefaultLLMProvider(workspace.ID, id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Default LLM provider updated"})
}

// TestLLMProvider tests if an LLM provider configuration is valid and reachable.
func TestLLMProvider(c *gin.Context) {
	id := getUintFromParam(c, "id")
	if id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid LLM provider ID"})
		return
	}

	provider, err := services.GetLLMProviderByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "LLM provider not found"})
		return
	}

	workspaceVal, exists := c.Get("current_workspace")
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "No workspace selected"})
		return
	}
	workspace, ok := workspaceVal.(*models.Workspace)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Invalid workspace"})
		return
	}

	userID := getUintFromContext(c, "user_id")
	isMember, _ := services.IsWorkspaceMember(workspace.ID, userID)
	if !isMember {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	hasPermission, err := services.CheckPermission(workspace.ID, userID, "can_manage_llm")
	if err != nil || !hasPermission {
		c.JSON(http.StatusForbidden, gin.H{"error": "Insufficient permissions"})
		return
	}

	// Test the provider
	testMessage, err := services.TestLLMProvider(provider)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Connection failed: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":  "Connection successful!",
		"provider": provider.Provider,
		"model":    provider.Model,
		"details":  testMessage,
	})
}


