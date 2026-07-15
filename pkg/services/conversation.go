package services

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"YourQL/pkg/models"
)

func CreateConversation(title string, llmProviderID, dbConnectionID *uint) (*models.Conversation, error) {
	now := time.Now().UTC()
	status := "active"
	result, err := models.DB.Exec(
		"INSERT INTO conversations (title, llm_provider_id, db_connection_id, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		title, llmProviderID, dbConnectionID, status, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create conversation: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get conversation ID: %w", err)
	}

	return &models.Conversation{
		ID:             uint(id),
		Title:          &title,
		Status:         status,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

func GetConversationByID(id uint) (*models.Conversation, error) {
	var c models.Conversation
	err := models.DB.QueryRow(
		"SELECT id, title, llm_provider_id, db_connection_id, status, max_messages, pinned, created_at, updated_at, tech_details FROM conversations WHERE id = ? LIMIT 1",
		id,
	).Scan(
		&c.ID, &c.Title, &c.LLMProviderID, &c.DBConnectionID,
		&c.Status, &c.MaxMessages, &c.Pinned, &c.CreatedAt, &c.UpdatedAt, &c.TechDetails,
	)
	if err == sql.ErrNoRows {
		return nil, errors.New("conversation not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get conversation: %w", err)
	}
	return &c, nil
}

func ListConversationsByUser() ([]*models.Conversation, error) {
	rows, err := models.DB.Query(
		"SELECT id, title, llm_provider_id, db_connection_id, status, max_messages, pinned, created_at, updated_at, tech_details FROM conversations WHERE status != 'deleted' ORDER BY pinned DESC, updated_at DESC",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list conversations: %w", err)
	}
	defer rows.Close()

	conversations := make([]*models.Conversation, 0)
	for rows.Next() {
		var c models.Conversation
		err := rows.Scan(
			&c.ID, &c.Title, &c.LLMProviderID, &c.DBConnectionID,
			&c.Status, &c.MaxMessages, &c.Pinned, &c.CreatedAt, &c.UpdatedAt, &c.TechDetails,
		)
		if err != nil {
			continue
		}
		conversations = append(conversations, &c)
	}
	return conversations, nil
}

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

func UpdateConversationTitle(id uint, title string) (*models.Conversation, error) {
	_, err := GetConversationByID(id)
	if err != nil {
		return nil, err
	}
	_, err = models.DB.Exec(
		"UPDATE conversations SET title = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		title, id,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to update conversation title: %w", err)
	}
	return GetConversationByID(id)
}

func UpdateConversationMaxMessages(id uint, maxMessages int) error {
	_, err := models.DB.Exec(
		"UPDATE conversations SET max_messages = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		maxMessages, id,
	)
	if err != nil {
		return fmt.Errorf("failed to update conversation max_messages: %w", err)
	}
	return nil
}

func UpdateConversationPinned(id uint, pinned bool) error {
	_, err := models.DB.Exec(
		"UPDATE conversations SET pinned = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		pinned, id,
	)
	if err != nil {
		return fmt.Errorf("failed to update conversation pinned: %w", err)
	}
	return nil
}

func DuplicateConversation(id uint) (*models.Conversation, error) {
	c, err := GetConversationByID(id)
	if err != nil {
		return nil, err
	}
	// Get original title and append " (copy)"
	newTitle := *c.Title + " (copy)"
	now := time.Now().UTC()
	result, err := models.DB.Exec(
		"INSERT INTO conversations (title, llm_provider_id, db_connection_id, status, max_messages, pinned, tech_details, created_at, updated_at) VALUES (?, ?, ?, 'active', ?, ?, ?, ?, ?)",
		newTitle, c.LLMProviderID, c.DBConnectionID, c.MaxMessages, c.Pinned, c.TechDetails, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to duplicate conversation: %w", err)
	}
	newID, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get new conversation ID: %w", err)
	}

	// Copy messages
	rows, err := models.DB.Query(
		"SELECT id, role, content, llm_content, sql_results, metadata, created_at FROM conversation_messages WHERE conversation_id = ? ORDER BY created_at ASC",
		id,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query messages for duplication: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var msg models.ConversationMessage
		var llmNull, sqlNull, metaNull sql.NullString
		err := rows.Scan(&msg.ID, &msg.Role, &msg.Content, &llmNull, &sqlNull, &metaNull, &msg.CreatedAt)
		if err != nil {
			continue
		}
		var llmPtr *string
		if llmNull.Valid {
			s := llmNull.String
			llmPtr = &s
		}
		var sqlPtr *string
		if sqlNull.Valid {
			s := sqlNull.String
			sqlPtr = &s
		}
		var metaPtr *string
		if metaNull.Valid {
			s := metaNull.String
			metaPtr = &s
		}
		_, err = models.DB.Exec(
			"INSERT INTO conversation_messages (conversation_id, role, content, llm_content, sql_results, metadata, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
			newID, msg.Role, msg.Content, llmPtr, sqlPtr, metaPtr, msg.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to copy message: %w", err)
		}
	}

	return GetConversationByID(uint(newID))
}

func CreateConversationMessage(conversationID uint, role, content string, llmContent *string, sqlResults *string, metadata *string) (*models.ConversationMessage, error) {
	now := time.Now().UTC()
	var metaArg interface{}
	var metaPtr *string
	if metadata == nil || *metadata == "" {
		metaArg = nil
		metaPtr = nil
	} else {
		metaArg = *metadata
		metaPtr = metadata
	}
	var llmArg interface{}
	var llmPtr *string
	if llmContent == nil || *llmContent == "" {
		llmArg = nil
		llmPtr = nil
	} else {
		llmArg = *llmContent
		llmPtr = llmContent
	}
	var sqlResultsArg interface{}
	if sqlResults == nil || *sqlResults == "" {
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

func DeleteConversationMessages(conversationID uint) error {
	_, err := models.DB.Exec(
		"DELETE FROM conversation_messages WHERE conversation_id = ?",
		conversationID,
	)
	return err
}
