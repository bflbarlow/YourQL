package services

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"YourQL/pkg/models"
)

func CreateConversation(title string, llmProviderID, dataSourceID *uint) (*models.Conversation, error) {
	now := time.Now().UTC()
	status := "active"
	result, err := models.DB.Exec(
		"INSERT INTO conversations (title, llm_provider_id, data_source_id, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		title, llmProviderID, dataSourceID, status, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create conversation: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get conversation ID: %w", err)
	}

	return &models.Conversation{
		ID:            uint(id),
		Title:         &title,
		LLMProviderID: llmProviderID,
		DataSourceID:  dataSourceID,
		Status:        status,
		CreatedAt:     now,
		UpdatedAt:     now,
	}, nil
}

// CreateConversationWithDefaults creates a conversation and applies the
// user's configured discussion defaults (LLM provider, data source, max
// messages, toggles, etc.) before returning. Defaults are a convenience —
// any field without a configured default keeps the system default.
func CreateConversationWithDefaults(title string, llmProviderID, dataSourceID *uint) (*models.Conversation, error) {
	defaults, _ := GetDiscussionDefaults()

	if llmProviderID == nil && defaults != nil {
		llmProviderID = defaults.LLMProviderID
	}
	if dataSourceID == nil && defaults != nil {
		dataSourceID = defaults.DataSourceID
	}

	conv, err := CreateConversation(title, llmProviderID, dataSourceID)
	if err != nil {
		return nil, err
	}

	if defaults != nil {
		if defaults.MaxMessages != nil {
			_ = UpdateConversationMaxMessages(conv.ID, *defaults.MaxMessages)
		}
		if defaults.MaxContextMessages != nil {
			_ = UpdateConversationMaxContextMessages(conv.ID, *defaults.MaxContextMessages)
		}
		if defaults.Summarize != nil {
			_ = UpdateConversationSummarize(conv.ID, *defaults.Summarize)
		}
		if defaults.VizEnabled != nil {
			_ = UpdateConversationVizEnabled(conv.ID, *defaults.VizEnabled)
		}
		if defaults.TechDetails != nil {
			_ = UpdateConversationTechDetails(conv.ID, *defaults.TechDetails)
		}
		if defaults.ContextDetails != nil {
			_ = UpdateConversationContextDetails(conv.ID, *defaults.ContextDetails)
		}
		if defaults.StreamingEnabled != nil {
			_ = UpdateConversationStreamingEnabled(conv.ID, *defaults.StreamingEnabled)
		}
	}

	// Apply system floor for MaxContextMessages when nothing was configured.
	// 0 = "no limit" is dangerous — it sends the entire conversation history
	// to the LLM, which blows context windows and causes empty/hallucinated
	// responses (see ANSWER_CLARIFICATION_ISSUE.md). The default of 5 keeps
	// enough context for follow-up questions without overwhelming the model.
	if conv.MaxContextMessages == 0 {
		_ = UpdateConversationMaxContextMessages(conv.ID, 5)
	}

	return GetConversationByID(conv.ID)
}

func GetConversationByID(id uint) (*models.Conversation, error) {
	var c models.Conversation
	err := models.DB.QueryRow(
		"SELECT id, title, llm_provider_id, data_source_id, status, max_messages, max_context_messages, pinned, created_at, updated_at, tech_details, context_details, summarize, viz_enabled, streaming_enabled FROM conversations WHERE id = ? LIMIT 1",
		id,
	).Scan(
		&c.ID, &c.Title, &c.LLMProviderID, &c.DataSourceID,
		&c.Status, &c.MaxMessages, &c.MaxContextMessages, &c.Pinned, &c.CreatedAt, &c.UpdatedAt, &c.TechDetails, &c.ContextDetails, &c.Summarize, &c.VizEnabled, &c.StreamingEnabled,
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
		"SELECT id, title, llm_provider_id, data_source_id, status, max_messages, max_context_messages, pinned, created_at, updated_at, tech_details, context_details, summarize, viz_enabled, streaming_enabled FROM conversations WHERE status != 'deleted' ORDER BY pinned DESC, updated_at DESC",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list conversations: %w", err)
	}
	defer rows.Close()

	conversations := make([]*models.Conversation, 0)
	for rows.Next() {
		var c models.Conversation
		err := rows.Scan(
			&c.ID, &c.Title, &c.LLMProviderID, &c.DataSourceID,
			&c.Status, &c.MaxMessages, &c.MaxContextMessages, &c.Pinned, &c.CreatedAt, &c.UpdatedAt, &c.TechDetails, &c.ContextDetails, &c.Summarize, &c.VizEnabled, &c.StreamingEnabled,
		)
		if err != nil {
			continue
		}
		conversations = append(conversations, &c)
	}
	return conversations, nil
}

func UpdateConversation(id uint, title *string, status *string, llmProviderID, dataSourceID *uint) (*models.Conversation, error) {
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
	newDataSourceID := c.DataSourceID
	if dataSourceID != nil {
		newDataSourceID = dataSourceID
	}

	_, err = models.DB.Exec(
		"UPDATE conversations SET title = ?, status = ?, llm_provider_id = ?, data_source_id = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		newTitle, newStatus, newLLMProviderID, newDataSourceID, id,
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

func UpdateConversationContextDetails(id uint, showContextDetails bool) error {
	_, err := models.DB.Exec(
		"UPDATE conversations SET context_details = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		showContextDetails, id,
	)
	if err != nil {
		return fmt.Errorf("failed to update conversation context_details: %w", err)
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

func UpdateConversationMaxContextMessages(id uint, maxContextMessages int) error {
	_, err := models.DB.Exec(
		"UPDATE conversations SET max_context_messages = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		maxContextMessages, id,
	)
	if err != nil {
		return fmt.Errorf("failed to update conversation max_context_messages: %w", err)
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

func UpdateConversationSummarize(id uint, summarize bool) error {
	_, err := models.DB.Exec(
		"UPDATE conversations SET summarize = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		summarize, id,
	)
	if err != nil {
		return fmt.Errorf("failed to update conversation summarize: %w", err)
	}
	return nil
}

func UpdateConversationVizEnabled(id uint, vizEnabled bool) error {
	_, err := models.DB.Exec(
		"UPDATE conversations SET viz_enabled = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		vizEnabled, id,
	)
	if err != nil {
		return fmt.Errorf("failed to update conversation viz_enabled: %w", err)
	}
	return nil
}

// UpdateConversationStreamingEnabled toggles streaming for a conversation.
func UpdateConversationStreamingEnabled(id uint, enabled bool) error {
	_, err := models.DB.Exec(
		"UPDATE conversations SET streaming_enabled = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		enabled, id,
	)
	if err != nil {
		return fmt.Errorf("failed to update conversation streaming_enabled: %w", err)
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
		"INSERT INTO conversations (title, llm_provider_id, data_source_id, status, max_messages, max_context_messages, pinned, tech_details, context_details, summarize, created_at, updated_at) VALUES (?, ?, ?, 'active', ?, ?, ?, ?, ?, ?, ?, ?)",
		newTitle, c.LLMProviderID, c.DataSourceID, c.MaxMessages, c.MaxContextMessages, c.Pinned, c.TechDetails, c.ContextDetails, c.Summarize, now, now,
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
		"SELECT id, role, content, llm_content, sql_results, metadata, tool_transcript, created_at FROM conversation_messages WHERE conversation_id = ? ORDER BY created_at ASC",
		id,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query messages for duplication: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var msg models.ConversationMessage
		var llmNull, sqlNull, metaNull, transcriptNull sql.NullString
		err := rows.Scan(&msg.ID, &msg.Role, &msg.Content, &llmNull, &sqlNull, &metaNull, &transcriptNull, &msg.CreatedAt)
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
		var transcriptPtr *string
		if transcriptNull.Valid {
			s := transcriptNull.String
			transcriptPtr = &s
		}
		_, err = models.DB.Exec(
			"INSERT INTO conversation_messages (conversation_id, role, content, llm_content, sql_results, metadata, tool_transcript, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
			newID, msg.Role, msg.Content, llmPtr, sqlPtr, metaPtr, transcriptPtr, msg.CreatedAt,
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
		"SELECT id, conversation_id, role, content, llm_content, sql_results, metadata, tool_transcript, created_at FROM conversation_messages WHERE conversation_id = ? ORDER BY created_at ASC",
		conversationID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get messages: %w", err)
	}
	defer rows.Close()

	messages := make([]*models.ConversationMessage, 0)
	for rows.Next() {
		var m models.ConversationMessage
		err := rows.Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content, &m.LLMContent, &m.SQLResults, &m.Metadata, &m.ToolTranscript, &m.CreatedAt)
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
