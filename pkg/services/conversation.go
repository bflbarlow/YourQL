package services

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"YourQL/pkg/models"
)

// CreateConversation creates a new discussion for a user in a workspace.
func CreateConversation(workspaceID, userID uint, title string, llmProviderID, dbConnectionID *uint) (*models.Conversation, error) {
	// Check user is a member of the workspace
	isMember, err := IsWorkspaceMember(workspaceID, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to check workspace membership: %w", err)
	}
	if !isMember {
		return nil, errors.New("user is not a member of this workspace")
	}

	// Check permission
	hasPermission, err := CheckPermission(workspaceID, userID, "can_create_conversations")
	if err != nil {
		return nil, err
	}
	if !hasPermission {
		return nil, errors.New("insufficient permissions to create conversations")
	}

	now := time.Now().UTC()
	status := "active"
	result, err := models.DB.Exec(
		"INSERT INTO conversations (workspace_id, user_id, title, llm_provider_id, db_connection_id, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		workspaceID, userID, title, llmProviderID, dbConnectionID, status, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create conversation: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get conversation ID: %w", err)
	}

	return &models.Conversation{
		ID:          uint(id),
		WorkspaceID: workspaceID,
		UserID:      userID,
		Title:       &title,
		Status:      status,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

// GetConversationByID retrieves a conversation by ID.
func GetConversationByID(id uint) (*models.Conversation, error) {
	var c models.Conversation
	err := models.DB.QueryRow(
		"SELECT id, workspace_id, user_id, title, llm_provider_id, db_connection_id, status, created_at, updated_at, tech_details FROM conversations WHERE id = ? LIMIT 1",
		id,
	).Scan(
		&c.ID, &c.WorkspaceID, &c.UserID, &c.Title, &c.LLMProviderID, &c.DBConnectionID,
		&c.Status, &c.CreatedAt, &c.UpdatedAt, &c.TechDetails,
	)
	if err == sql.ErrNoRows {
		return nil, errors.New("conversation not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get conversation: %w", err)
	}
	return &c, nil
}

// ListConversationsByUser lists all conversations for a user in a workspace.
func ListConversationsByUser(workspaceID, userID uint) ([]*models.Conversation, error) {
	// List with deleted_at filter
	rows, err := models.DB.Query(
		"SELECT id, workspace_id, user_id, title, llm_provider_id, db_connection_id, status, created_at, updated_at, tech_details FROM conversations WHERE workspace_id = ? AND user_id = ? AND status != 'deleted' ORDER BY updated_at DESC",
		workspaceID, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list conversations: %w", err)
	}
	defer rows.Close()

	conversations := make([]*models.Conversation, 0)
	for rows.Next() {
		var c models.Conversation
		err := rows.Scan(
			&c.ID, &c.WorkspaceID, &c.UserID, &c.Title, &c.LLMProviderID, &c.DBConnectionID,
			&c.Status, &c.CreatedAt, &c.UpdatedAt, &c.TechDetails,
		)
		if err != nil {
			continue
		}
		conversations = append(conversations, &c)
	}
	return conversations, nil
}

// ListAllConversations lists all conversations in a workspace (for admin).
func ListAllConversations(workspaceID uint) ([]*models.Conversation, error) {
	rows, err := models.DB.Query(
		"SELECT id, workspace_id, user_id, title, llm_provider_id, db_connection_id, status, created_at, updated_at, tech_details FROM conversations WHERE workspace_id = ? AND status != 'deleted' ORDER BY updated_at DESC",
		workspaceID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list conversations: %w", err)
	}
	defer rows.Close()

	conversations := make([]*models.Conversation, 0)
	for rows.Next() {
		var c models.Conversation
		err := rows.Scan(
			&c.ID, &c.WorkspaceID, &c.UserID, &c.Title, &c.LLMProviderID, &c.DBConnectionID,
			&c.Status, &c.CreatedAt, &c.UpdatedAt, &c.TechDetails,
		)
		if err != nil {
			continue
		}
		conversations = append(conversations, &c)
	}
	return conversations, nil
}

// UpdateConversation updates a conversation's title, status, or integration settings.
func UpdateConversation(id uint, title *string, status *string, llmProviderID, dbConnectionID *uint) (*models.Conversation, error) {
	c, err := GetConversationByID(id)
	if err != nil {
		return nil, err
	}

	newTitle := c.Title
	if title != nil {
		newTitle = title
	}
	newStatus := c.Status
	if status != nil {
		newStatus = *status
	}
	newLLMProviderID := c.LLMProviderID
	if llmProviderID != nil {
		newLLMProviderID = llmProviderID
	}
	newDBConnectionID := c.DBConnectionID
	if dbConnectionID != nil {
		newDBConnectionID = dbConnectionID
	}

	_, err = models.DB.Exec(
		"UPDATE conversations SET title = ?, status = ?, llm_provider_id = ?, db_connection_id = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		newTitle, newStatus, newLLMProviderID, newDBConnectionID, id,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to update conversation: %w", err)
	}

	return GetConversationByID(id)
}

// DeleteConversation soft-deletes a conversation by setting the status.
func DeleteConversation(id uint) error {
	_, err := models.DB.Exec(
		"UPDATE conversations SET status = 'deleted', deleted_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		id,
	)
	if err != nil {
		return fmt.Errorf("failed to delete conversation: %w", err)
	}
	return nil
}

// SoftDeleteConversation sets the deleted_at timestamp and marks the conversation as deleted.
func SoftDeleteConversation(id uint) error {
	now := time.Now().UTC()
	_, err := models.DB.Exec(
		"UPDATE conversations SET status = 'deleted', deleted_at = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		now, id,
	)
	if err != nil {
		return fmt.Errorf("failed to soft‑delete conversation: %w", err)
	}
	return nil
}

// ArchiveConversation archives a conversation.
func ArchiveConversation(id uint) error {
	_, err := models.DB.Exec(
		"UPDATE conversations SET status = 'archived', updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		id,
	)
	if err != nil {
		return fmt.Errorf("failed to archive conversation: %w", err)
	}
	return nil
}

// RestoreConversation restores a deleted conversation.
func RestoreConversation(id uint) error {
	_, err := models.DB.Exec(
		"UPDATE conversations SET status = 'active', updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		id,
	)
	if err != nil {
		return fmt.Errorf("failed to restore conversation: %w", err)
	}
	return nil
}

// UpdateConversationTechDetails updates the tech_details toggle for a conversation.
func UpdateConversationTechDetails(id uint, showTechDetails bool) error {
	_, err := models.DB.Exec(
		"UPDATE conversations SET tech_details = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		showTechDetails, id,
	)
	if err != nil {
		return fmt.Errorf("failed to update conversation tech_details: %w", err)
	}
	return nil
}

// CreateConversationMessage creates a new message in a conversation.
func CreateConversationMessage(conversationID uint, role, content string, llmContent *string, sqlResults *string, metadata *string) (*models.ConversationMessage, error) {
	now := time.Now().UTC()
	var metaArg interface{}
	var metaPtr *string
	if metadata == nil {
		metaArg = nil
		metaPtr = nil
	} else if *metadata == "" {
		metaArg = nil
		metaPtr = nil
	} else {
		metaArg = *metadata
		metaPtr = metadata
	}
	var llmArg interface{}
	var llmPtr *string
	if llmContent == nil {
		llmArg = nil
		llmPtr = nil
	} else if *llmContent == "" {
		llmArg = nil
		llmPtr = nil
	} else {
		llmArg = *llmContent
		llmPtr = llmContent
	}
	var sqlResultsArg interface{}
	if sqlResults == nil {
		sqlResultsArg = nil
	} else if *sqlResults == "" {
		sqlResultsArg = nil
	} else {
		sqlResultsArg = *sqlResults
	}
	result, err := models.DB.Exec(
		"INSERT INTO conversation_messages (conversation_id, role, content, llm_content, sql_results, metadata, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		conversationID, role, content, llmArg, sqlResultsArg, metaArg, now,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create message: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get message ID: %w", err)
	}

	// Update conversation's updated_at
	_, err = models.DB.Exec(
		"UPDATE conversations SET updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		conversationID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to update conversation: %w", err)
	}

	return &models.ConversationMessage{
		ID:             uint(id),
		ConversationID: conversationID,
		Role:           role,
		Content:        content,
		LLMContent:     llmPtr,
		SQLResults:     sqlResults,
		Metadata:       metaPtr,
		CreatedAt:      now,
	}, nil
}

// GetConversationMessages retrieves all messages in a conversation.
func GetConversationMessages(conversationID uint) ([]*models.ConversationMessage, error) {
	rows, err := models.DB.Query(
		"SELECT id, conversation_id, role, content, llm_content, sql_results, metadata, created_at FROM conversation_messages WHERE conversation_id = ? ORDER BY created_at ASC",
		conversationID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get messages: %w", err)
	}
	defer rows.Close()

	messages := make([]*models.ConversationMessage, 0)
	for rows.Next() {
		var m models.ConversationMessage
		err := rows.Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content, &m.LLMContent, &m.SQLResults, &m.Metadata, &m.CreatedAt)
		if err != nil {
			continue
		}
		messages = append(messages, &m)
	}
	return messages, nil
}

// DeleteConversationMessages deletes all messages in a conversation.
func DeleteConversationMessages(conversationID uint) error {
	_, err := models.DB.Exec(
		"DELETE FROM conversation_messages WHERE conversation_id = ?",
		conversationID,
	)
	return err
}

// GetConversationMessageByID retrieves a single message by ID.
func GetConversationMessageByID(id uint) (*models.ConversationMessage, error) {
	var m models.ConversationMessage
	err := models.DB.QueryRow(
		"SELECT id, conversation_id, role, content, llm_content, metadata, created_at FROM conversation_messages WHERE id = ? LIMIT 1",
		id,
	).Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content, &m.LLMContent, &m.Metadata, &m.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, errors.New("message not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get message: %w", err)
	}
	return &m, nil
}

// CountConversationsByUser returns the number of conversations for a user in a workspace.
func CountConversationsByUser(workspaceID, userID uint) (int, error) {
	var count int
	err := models.DB.QueryRow(
		"SELECT COUNT(*) FROM conversations WHERE workspace_id = ? AND user_id = ? AND status = 'active'",
		workspaceID, userID,
	).Scan(&count)
	return count, err
}
