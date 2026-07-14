package controllers

import (
	"net/http"
	"strconv"
	"time"

	"YourQL/pkg/models"
	"YourQL/pkg/services"
	"YourQL/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
)

// ListDiscussions lists all discussions for the current user in the current workspace.
func ListDiscussions(c *gin.Context) {
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

	discussions, err := services.ListConversationsByUser(workspace.ID, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load discussions"})
		return
	}

	type discussionSummary struct {
		ID           uint      `json:"id"`
		Title        string    `json:"title"`
		MessageCount int       `json:"message_count"`
		CreatedAt    time.Time `json:"created_at"`
		UpdatedAt    time.Time `json:"updated_at"`
	}

	var summaries []discussionSummary
	for _, d := range discussions {
		title := ""
		if d.Title != nil {
			title = *d.Title
		}
		summaries = append(summaries, discussionSummary{
			ID:        d.ID,
			Title:     title,
			CreatedAt: d.CreatedAt,
			UpdatedAt: d.UpdatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{"discussions": summaries})
}

// GetDiscussion retrieves a single discussion by ID.
func GetDiscussion(c *gin.Context) {
	id := getUintFromParam(c, "id")
	if id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid discussion ID"})
		return
	}

	discussion, err := services.GetConversationByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Discussion not found"})
		return
	}

	userID := getUintFromContext(c, "user_id")
	isOwner := discussion.UserID == userID
	isAdmin, _ := services.IsWorkspaceOwner(discussion.WorkspaceID, userID)
	isMember, _ := services.IsWorkspaceMember(discussion.WorkspaceID, userID)

	if !isOwner && !isMember && !isAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	messages, err := services.GetConversationMessages(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load messages"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"discussion": discussion,
		"messages":   messages,
		"owner":      isOwner,
	})
}

// CreateDiscussion creates a new discussion.
func CreateDiscussion(c *gin.Context) {
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

	var input struct {
		Title          string `json:"title" form:"title" binding:"required"`
		LLMProviderID  *uint  `json:"llm_provider_id" form:"llm_provider_id"`
		DBConnectionID *uint  `json:"db_connection_id" form:"db_connection_id"`
	}

	// Try JSON first, fall back to form data
	if err := c.ShouldBindJSON(&input); err != nil {
		// Fall back to form data binding
		if bindErr := c.ShouldBindWith(&input, binding.Form); bindErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Title is required"})
			return
		}
	}

	// Use workspace defaults if not specified
	var llmProviderID, dbConnectionID *uint
	if input.LLMProviderID != nil && *input.LLMProviderID != 0 {
		llmProviderID = input.LLMProviderID
	}
	if input.DBConnectionID != nil && *input.DBConnectionID != 0 {
		dbConnectionID = input.DBConnectionID
	}

	discussion, err := services.CreateConversation(workspace.ID, userID, input.Title, llmProviderID, dbConnectionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"discussion": discussion})
}

// UpdateDiscussion updates a discussion's title.
func UpdateDiscussion(c *gin.Context) {
	id := getUintFromParam(c, "id")
	if id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid discussion ID"})
		return
	}

	discussion, err := services.GetConversationByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Discussion not found"})
		return
	}

	userID := getUintFromContext(c, "user_id")
	if discussion.UserID != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	var input struct {
		Title          string `json:"title"`
		LLMProviderID  *uint  `json:"llm_provider_id"`
		DBConnectionID *uint  `json:"db_connection_id"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input"})
		return
	}

	// Use workspace defaults if not specified
	var llmProviderID, dbConnectionID *uint
	if input.LLMProviderID != nil && *input.LLMProviderID != 0 {
		llmProviderID = input.LLMProviderID
	}
	if input.DBConnectionID != nil && *input.DBConnectionID != 0 {
		dbConnectionID = input.DBConnectionID
	}

	if input.Title != "" {
		_, err := services.UpdateConversation(id, &input.Title, nil, llmProviderID, dbConnectionID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update discussion"})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "Discussion updated"})
}

// ArchiveDiscussion archives a discussion.
func ArchiveDiscussion(c *gin.Context) {
	id := getUintFromParam(c, "id")
	if id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid discussion ID"})
		return
	}

	discussion, err := services.GetConversationByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Discussion not found"})
		return
	}

	userID := getUintFromContext(c, "user_id")
	if discussion.UserID != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	if err := services.ArchiveConversation(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to archive discussion"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Discussion archived"})
}

// DeleteDiscussion deletes a discussion.
func DeleteDiscussion(c *gin.Context) {
	id := getUintFromParam(c, "id")
	if id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid discussion ID"})
		return
	}

	discussion, err := services.GetConversationByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Discussion not found"})
		return
	}

	userID := getUintFromContext(c, "user_id")
	if discussion.UserID != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	if err := services.DeleteConversation(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete discussion"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Discussion deleted"})
}

// CreateMessage creates a new message in a discussion.
func CreateMessage(c *gin.Context) {
	discussionID := getUintFromParam(c, "id")
	if discussionID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid discussion ID"})
		return
	}

	discussion, err := services.GetConversationByID(discussionID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Discussion not found"})
		return
	}

	userID := getUintFromContext(c, "user_id")
	isMember, _ := services.IsWorkspaceMember(discussion.WorkspaceID, userID)
	if !isMember {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	var input struct {
		Content string `json:"content" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Content is required"})
		return
	}

	message, err := services.CreateConversationMessage(discussionID, "user", input.Content, nil, nil, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create message"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": message})
}

// GetMessages retrieves all messages in a discussion.
func GetMessages(c *gin.Context) {
	discussionID := getUintFromParam(c, "id")
	if discussionID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid discussion ID"})
		return
	}

	discussion, err := services.GetConversationByID(discussionID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Discussion not found"})
		return
	}

	userID := getUintFromContext(c, "user_id")
	isMember, _ := services.IsWorkspaceMember(discussion.WorkspaceID, userID)
	if !isMember {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	messages, err := services.GetConversationMessages(discussionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load messages"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"messages": messages})
}

// getUintFromParam extracts a uint parameter from the Gin context.
func getUintFromParam(c *gin.Context, key string) uint {
	val := c.Param(key)
	if val == "" {
		return 0
	}
	i, err := strconv.ParseUint(val, 10, 32)
	if err != nil {
		return 0
	}
	return uint(i)
}

// getUintFromContext extracts a uint value from the Gin context, handling float64 conversion.
func getUintFromContext(c *gin.Context, key string) uint {
	val, exists := c.Get(key)
	if !exists {
		return 0
	}
	switch v := val.(type) {
	case uint:
		return v
	case float64:
		return uint(v)
	case int:
		return uint(v)
	default:
		return 0
	}
}

// sanitizeInput trims whitespace from a string.
func sanitizeInput(s string) string {
	return utils.SanitizeEmail(s)
}
