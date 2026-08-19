package services

import (
	"database/sql"
	"fmt"
	"strings"

	"YourQL/pkg/models"
)

const maxTagLength = 50

// AddTagToConversation adds a tag to a conversation. Tag names are trimmed,
// validated (no commas or pipes, ≤ 50 chars), and case-insensitively unique.
// If the tag doesn't exist yet, it's created.
func AddTagToConversation(conversationID uint, tagName string) error {
	name := strings.TrimSpace(tagName)
	if name == "" {
		return fmt.Errorf("tag name cannot be empty")
	}
	if len(name) > maxTagLength {
		return fmt.Errorf("tag name too long (max %d characters)", maxTagLength)
	}
	if strings.Contains(name, ",") || strings.Contains(name, "|") {
		return fmt.Errorf("tag name cannot contain commas or pipes")
	}

	// Ensure tag exists (case-insensitive — COLLATE NOCASE handles the UNIQUE)
	_, err := models.DB.Exec("INSERT OR IGNORE INTO tags (name) VALUES (?)", name)
	if err != nil {
		return fmt.Errorf("failed to create tag: %w", err)
	}

	// Get the tag ID (must query since INSERT OR IGNORE doesn't return the id
	// of an existing row)
	var tagID int64
	err = models.DB.QueryRow("SELECT id FROM tags WHERE name = ? COLLATE NOCASE", name).Scan(&tagID)
	if err != nil {
		return fmt.Errorf("failed to find tag: %w", err)
	}

	// Associate with conversation (ignore duplicates)
	_, err = models.DB.Exec("INSERT OR IGNORE INTO conversation_tags (conversation_id, tag_id) VALUES (?, ?)",
		conversationID, tagID)
	if err != nil {
		return fmt.Errorf("failed to associate tag with conversation: %w", err)
	}
	return nil
}

// RemoveTagFromConversation removes a tag from a conversation. If the tag is
// no longer used by any conversation, it's deleted (orphan cleanup).
func RemoveTagFromConversation(conversationID uint, tagName string) error {
	name := strings.TrimSpace(tagName)
	if name == "" {
		return nil
	}

	// Find the tag
	var tagID int64
	err := models.DB.QueryRow("SELECT id FROM tags WHERE name = ? COLLATE NOCASE", name).Scan(&tagID)
	if err == sql.ErrNoRows {
		return nil // tag doesn't exist, nothing to remove
	}
	if err != nil {
		return fmt.Errorf("failed to find tag: %w", err)
	}

	// Remove the association
	_, err = models.DB.Exec("DELETE FROM conversation_tags WHERE conversation_id = ? AND tag_id = ?",
		conversationID, tagID)
	if err != nil {
		return fmt.Errorf("failed to remove tag from conversation: %w", err)
	}

	// Cleanup orphaned tags
	cleanupOrphanTags()
	return nil
}

// GetTagsForConversation returns all tag names for a conversation, sorted.
func GetTagsForConversation(conversationID uint) ([]string, error) {
	rows, err := models.DB.Query(
		`SELECT t.name FROM tags t
		 JOIN conversation_tags ct ON ct.tag_id = t.id
		 WHERE ct.conversation_id = ?
		 ORDER BY t.name`, conversationID)
	if err != nil {
		return nil, fmt.Errorf("failed to query tags: %w", err)
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		tags = append(tags, name)
	}
	if tags == nil {
		tags = []string{}
	}
	return tags, nil
}

// ListAllTags returns every tag name in the database, sorted alphabetically.
func ListAllTags() ([]string, error) {
	rows, err := models.DB.Query("SELECT name FROM tags ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("failed to list tags: %w", err)
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		tags = append(tags, name)
	}
	if tags == nil {
		tags = []string{}
	}
	return tags, nil
}

// ListConversationsWithTags returns all conversations with their tags
// pre-resolved via a GROUP_CONCAT subquery. Replaces ListConversationsByUser.
func ListConversationsWithTags() ([]*models.Conversation, error) {
	rows, err := models.DB.Query(
		`SELECT c.id, c.title, c.llm_provider_id, c.data_source_id,
		        c.status, c.max_messages, c.max_context_messages, c.pinned,
		        c.created_at, c.updated_at, c.tech_details, c.context_details,
		        c.summarize, c.viz_enabled, c.streaming_enabled,
		        COALESCE(GROUP_CONCAT(t.name, '||'), '') as tag_names
		 FROM conversations c
		 LEFT JOIN conversation_tags ct ON ct.conversation_id = c.id
		 LEFT JOIN tags t ON t.id = ct.tag_id
		 WHERE c.status != 'deleted'
		 GROUP BY c.id
		 ORDER BY c.pinned DESC, c.updated_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("failed to list conversations: %w", err)
	}
	defer rows.Close()

	conversations := make([]*models.Conversation, 0)
	for rows.Next() {
		var c models.Conversation
		var tagNames string
		err := rows.Scan(
			&c.ID, &c.Title, &c.LLMProviderID, &c.DataSourceID,
			&c.Status, &c.MaxMessages, &c.MaxContextMessages, &c.Pinned,
			&c.CreatedAt, &c.UpdatedAt, &c.TechDetails, &c.ContextDetails,
			&c.Summarize, &c.VizEnabled, &c.StreamingEnabled, &tagNames,
		)
		if err != nil {
			continue
		}
		if tagNames != "" {
			c.Tags = strings.Split(tagNames, "||")
		}
		conversations = append(conversations, &c)
	}
	return conversations, nil
}

// cleanupOrphanTags removes tags that are no longer used by any conversation.
func cleanupOrphanTags() {
	_, _ = models.DB.Exec(
		"DELETE FROM tags WHERE id NOT IN (SELECT DISTINCT tag_id FROM conversation_tags)")
}