package controllers

import (
	"fmt"
	"net/http"
	"strings"

	"YourQL/pkg/models"
	"YourQL/pkg/services"

	"github.com/gin-gonic/gin"
)

// ListDBConnections lists all database connections for the current workspace.
func ListDBConnections(c *gin.Context) {
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

	connections, err := services.ListDBConnectionsByWorkspace(workspace.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load database connections"})
		return
	}

	type connectionSummary struct {
		ID          uint   `json:"id"`
		Name        string `json:"name"`
		Type        string `json:"type"`
		Host        string `json:"host"`
		Port        int    `json:"port"`
		Database    string `json:"database"`
		IsDefault   bool   `json:"is_default"`
		IsActive    bool   `json:"is_active"`
		CreatedAt   string `json:"created_at"`
		UpdatedAt   string `json:"updated_at"`
	}

	var summaries []connectionSummary
	for _, c := range connections {
		host := ""
		if c.Host != nil {
			host = *c.Host
		}
		port := 0
		if c.Port != nil {
			port = *c.Port
		}
		database := ""
		if c.Database != nil {
			database = *c.Database
		}
		summaries = append(summaries, connectionSummary{
			ID:        c.ID,
			Name:      c.Name,
			Type:      c.Type,
			Host:      host,
			Port:      port,
			Database:  database,
			IsDefault: c.IsDefault,
			IsActive:  c.IsActive,
			CreatedAt: c.CreatedAt.Format("Jan 2, 2006"),
			UpdatedAt: c.UpdatedAt.Format("Jan 2, 2006"),
		})
	}

	c.JSON(http.StatusOK, gin.H{"connections": summaries})
}

// GetDBConnection retrieves a single database connection by ID.
func GetDBConnection(ctx *gin.Context) {
	id := getUintFromParam(ctx, "id")
	if id == 0 {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid database connection ID"})
		return
	}

	conn, err := services.GetDBConnectionByID(id)
	if err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "Database connection not found"})
		return
	}

	workspaceVal, exists := ctx.Get("current_workspace")
	if !exists {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "No workspace selected"})
		return
	}
	workspace, ok := workspaceVal.(*models.Workspace)
	if !ok {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "Invalid workspace"})
		return
	}

	userID := getUintFromContext(ctx, "user_id")
	isMember, _ := services.IsWorkspaceMember(workspace.ID, userID)
	if !isMember {
		ctx.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	host := ""
	if conn.Host != nil {
		host = *conn.Host
	}
	port := 0
	if conn.Port != nil {
		port = *conn.Port
	}
	database := ""
	if conn.Database != nil {
		database = *conn.Database
	}

	ctx.JSON(http.StatusOK, gin.H{
		"connection": gin.H{
			"id":         conn.ID,
			"name":       conn.Name,
			"type":       conn.Type,
			"host":       host,
			"port":       port,
			"database":   database,
			"is_default": conn.IsDefault,
			"is_active":  conn.IsActive,
			"created_at": conn.CreatedAt.Format("Jan 2, 2006"),
			"updated_at": conn.UpdatedAt.Format("Jan 2, 2006"),
		},
	})
}

// CreateDBConnection creates a new database connection.
func CreateDBConnection(c *gin.Context) {
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

	hasPermission, err := services.CheckPermission(workspace.ID, userID, "can_manage_db")
	if err != nil || !hasPermission {
		c.JSON(http.StatusForbidden, gin.H{"error": "Insufficient permissions"})
		return
	}

	var input struct {
		Name     string `json:"name" binding:"required"`
		Type     string `json:"type" binding:"required"`
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Database string `json:"database"`
		Username string `json:"username"`
		Password string `json:"password"`
		SSLMode  string `json:"ssl_mode"`
		Config   string `json:"config"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input"})
		return
	}

	conn, err := services.CreateDBConnection(workspace.ID, userID, input.Name, input.Type, input.Host, input.Port, input.Database, input.Username, input.Password, input.SSLMode, input.Config)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"connection": conn})
}

// UpdateDBConnection updates a database connection.
func UpdateDBConnection(ctx *gin.Context) {
	id := getUintFromParam(ctx, "id")
	if id == 0 {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid database connection ID"})
		return
	}

	_, err := services.GetDBConnectionByID(id)
	if err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "Database connection not found"})
		return
	}

	workspaceVal, exists := ctx.Get("current_workspace")
	if !exists {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "No workspace selected"})
		return
	}
	workspace, ok := workspaceVal.(*models.Workspace)
	if !ok {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "Invalid workspace"})
		return
	}

	userID := getUintFromContext(ctx, "user_id")
	isMember, _ := services.IsWorkspaceMember(workspace.ID, userID)
	if !isMember {
		ctx.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	hasPermission, err := services.CheckPermission(workspace.ID, userID, "can_manage_db")
	if err != nil || !hasPermission {
		ctx.JSON(http.StatusForbidden, gin.H{"error": "Insufficient permissions"})
		return
	}

	var input struct {
		Name     *string `json:"name"`
		Host     *string `json:"host"`
		Port     *int    `json:"port"`
		Database *string `json:"database"`
		Username *string `json:"username"`
		Password *string `json:"password"`
		SSLMode  *string `json:"ssl_mode"`
		Config   *string `json:"config"`
	}
	if err := ctx.ShouldBindJSON(&input); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input"})
		return
	}

	updated, err := services.UpdateDBConnection(id, input.Name, input.Host, input.Port, input.Database, input.Username, input.Password, input.SSLMode, input.Config)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"connection": updated})
}

// DeleteDBConnection deletes a database connection.
func DeleteDBConnection(c *gin.Context) {
	id := getUintFromParam(c, "id")
	if id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid database connection ID"})
		return
	}

	_, err := services.GetDBConnectionByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Database connection not found"})
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

	hasPermission, err := services.CheckPermission(workspace.ID, userID, "can_manage_db")
	if err != nil || !hasPermission {
		c.JSON(http.StatusForbidden, gin.H{"error": "Insufficient permissions"})
		return
	}

	if err := services.DeleteDBConnection(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Database connection deleted"})
}

// PreviewSystemPrompt returns the full system prompt that would be used for a connection.
func PreviewSystemPrompt(c *gin.Context) {
	id := getUintFromParam(c, "id")
	if id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid database connection ID"})
		return
	}

	conn, err := services.GetDBConnectionByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Database connection not found"})
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

	// Parse config
	config, _ := conn.ParseConfig()

	// Build the full system prompt that would be used
	var sb strings.Builder

	// Custom system prompt
	if config.SystemPrompt != "" {
		sb.WriteString(config.SystemPrompt)
		sb.WriteString("\n\n")
	}

	// Business rules
	if len(config.BusinessRules) > 0 {
		sb.WriteString("## Business Rules\n")
		for _, rule := range config.BusinessRules {
			sb.WriteString(fmt.Sprintf("- %s\n", rule))
		}
		sb.WriteString("\n")
	}

	// Table descriptions
	if config.TableDescriptions != nil && len(config.TableDescriptions) > 0 {
		sb.WriteString("## Table Descriptions\n")
		for tableName, desc := range config.TableDescriptions {
			sb.WriteString(fmt.Sprintf("- **%s**: %s\n", tableName, desc))
		}
		sb.WriteString("\n")
	}

	// Default instructions (what the LLM always sees)
	sb.WriteString("## Instructions\n")
	sb.WriteString("1. Analyze the user's question and the database schema (if provided).\n")
	sb.WriteString("2. Respond with a JSON object containing exactly the following fields:\n")
	sb.WriteString("   - \"action\": either \"sql_query\" or \"clarification\"\n")
	sb.WriteString("   - \"sql_query\": if action is \"sql_query\", provide a valid SQL query (SELECT only).\n")
	sb.WriteString("   - \"clarification_question\": if action is \"clarification\", ask a concise clarifying question.\n")
	sb.WriteString("   - \"explanation\": optional short explanation.\n")
	sb.WriteString("3. The SQL query must be safe, read-only, and compatible with the database type (MySQL).\n")
	sb.WriteString("4. If the user asks a general question not related to the database, you may answer directly.\n")
	sb.WriteString("\nYour response must be a valid JSON object, no additional text.\n")

	c.JSON(http.StatusOK, gin.H{
		"prompt": sb.String(),
	})
}

// TestDBConnection tests if a database connection is valid and reachable, and returns the full schema.
func TestDBConnection(c *gin.Context) {
	id := getUintFromParam(c, "id")
	if id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid database connection ID"})
		return
	}

	conn, err := services.GetDBConnectionByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Database connection not found"})
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

	hasPermission, err := services.CheckPermission(workspace.ID, userID, "can_manage_db")
	if err != nil || !hasPermission {
		c.JSON(http.StatusForbidden, gin.H{"error": "Insufficient permissions"})
		return
	}

	// Test the connection
	err = services.TestDBConnection(conn)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Connection failed: " + err.Error()})
		return
	}

	// Get full schema info for display
	schema, err := services.GetDatabaseSchema(conn)
	var tableCount int64
	var columnCount int64
	if err == nil && schema != nil {
		tableCount = int64(len(schema.Tables))
		for _, t := range schema.Tables {
			columnCount += int64(len(t.Columns))
		}
	}

	// Build schema tables for the UI
	type uiColumn struct {
		Name         string `json:"name"`
		DataType     string `json:"data_type"`
		IsNullable   bool   `json:"is_nullable"`
		IsPrimaryKey bool   `json:"is_primary_key"`
		DefaultValue string `json:"default_value,omitempty"`
	}
	type uiTable struct {
		Name        string     `json:"name"`
		RowCount    int64      `json:"row_count"`
		Description string     `json:"description"`
		Columns     []uiColumn `json:"columns"`
	}

	var tables []uiTable
	if err == nil && schema != nil {
		for _, t := range schema.Tables {
			var cols []uiColumn
			for _, col := range t.Columns {
				cols = append(cols, uiColumn{
					Name:         col.Name,
					DataType:     col.DataType,
					IsNullable:   col.IsNullable,
					IsPrimaryKey: col.IsPrimaryKey,
					DefaultValue: col.DefaultValue,
				})
			}
			tables = append(tables, uiTable{
				Name:        t.Name,
				RowCount:    t.RowCount,
				Description: t.Description,
				Columns:     cols,
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"message":       "Connection successful!",
		"type":          conn.Type,
		"tableCount":    tableCount,
		"columnCount":   columnCount,
		"schemaLoaded":  err == nil && schema != nil,
		"tables":         tables,
	})
}

// ImportSchema imports the database schema for a connection.
func ImportSchema(c *gin.Context) {
	id := getUintFromParam(c, "id")
	if id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid database connection ID"})
		return
	}

	conn, err := services.GetDBConnectionByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Database connection not found"})
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

	hasPermission, err := services.CheckPermission(workspace.ID, userID, "can_manage_db")
	if err != nil || !hasPermission {
		c.JSON(http.StatusForbidden, gin.H{"error": "Insufficient permissions"})
		return
	}

	// Get the schema
	schema, err := services.GetDatabaseSchema(conn)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to import schema: " + err.Error()})
		return
	}

	// Build table info for the UI
	type columnInfo struct {
		Name         string `json:"name"`
		DataType     string `json:"data_type"`
		IsPrimaryKey bool   `json:"is_primary_key"`
		IsNullable   bool   `json:"is_nullable"`
	}
	type tableInfo struct {
		Name        string       `json:"name"`
		RowCount    int64        `json:"row_count"`
		Columns     []columnInfo `json:"columns"`
		Description string       `json:"description"`
	}

	var tables []tableInfo
	for _, t := range schema.Tables {
		var cols []columnInfo
		for _, col := range t.Columns {
			cols = append(cols, columnInfo{
				Name:         col.Name,
				DataType:     col.DataType,
				IsPrimaryKey: col.IsPrimaryKey,
				IsNullable:   col.IsNullable,
			})
		}
		tables = append(tables, tableInfo{
			Name:        t.Name,
			RowCount:    t.RowCount,
			Columns:     cols,
			Description: t.Description,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"tables": tables,
		"count":  len(tables),
	})
}

// SetDefaultDBConnection sets a database connection as the default.
func SetDefaultDBConnection(c *gin.Context) {
	id := getUintFromParam(c, "id")
	if id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid database connection ID"})
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

	hasPermission, err := services.CheckPermission(workspace.ID, userID, "can_manage_db")
	if err != nil || !hasPermission {
		c.JSON(http.StatusForbidden, gin.H{"error": "Insufficient permissions"})
		return
	}

	if err := services.SetDefaultDBConnection(workspace.ID, id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Default database connection updated"})
}


