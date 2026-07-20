package services

import (
	"fmt"
	"strings"

	"YourQL/pkg/models"
)

// ListSkills returns all skills ordered by name.
func ListSkills() ([]models.Skill, error) {
	rows, err := models.DB.Query(
		"SELECT id, name, markdown_content, is_active, created_at, updated_at FROM skills ORDER BY name",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var skills []models.Skill
	for rows.Next() {
		var s models.Skill
		if err := rows.Scan(&s.ID, &s.Name, &s.MarkdownContent, &s.IsActive, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		skills = append(skills, s)
	}
	return skills, rows.Err()
}

// CreateSkill creates a new skill.
func CreateSkill(name, markdownContent string) (*models.Skill, error) {
	result, err := models.DB.Exec(
		"INSERT INTO skills (name, markdown_content) VALUES (?, ?)",
		name, markdownContent,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create skill: %w", err)
	}
	id, _ := result.LastInsertId()
	return GetSkill(uint(id))
}

// UpdateSkill updates a skill's name and markdown content.
func UpdateSkill(id uint, name, markdownContent string) (*models.Skill, error) {
	_, err := models.DB.Exec(
		"UPDATE skills SET name = ?, markdown_content = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		name, markdownContent, id,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to update skill: %w", err)
	}
	return GetSkill(id)
}

// DeleteSkill deletes a skill and its conversation associations (via CASCADE).
func DeleteSkill(id uint) error {
	_, err := models.DB.Exec("DELETE FROM skills WHERE id = ?", id)
	return err
}

// SetSkillActive globally enables or disables a skill.
func SetSkillActive(id uint, active bool) error {
	_, err := models.DB.Exec(
		"UPDATE skills SET is_active = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		active, id,
	)
	return err
}

// GetSkill returns a single skill by ID.
func GetSkill(id uint) (*models.Skill, error) {
	var s models.Skill
	err := models.DB.QueryRow(
		"SELECT id, name, markdown_content, is_active, created_at, updated_at FROM skills WHERE id = ?", id,
	).Scan(&s.ID, &s.Name, &s.MarkdownContent, &s.IsActive, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// GetEnabledSkillsContent returns the markdown content of all enabled
// skills for a conversation, concatenated with double newlines.
func GetEnabledSkillsContent(conversationID uint) (string, error) {
	rows, err := models.DB.Query(`
		SELECT s.markdown_content FROM skills s
		JOIN conversation_skills cs ON cs.skill_id = s.id
		WHERE cs.conversation_id = ? AND cs.enabled = 1 AND s.is_active = 1
		ORDER BY s.name
	`, conversationID)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var parts []string
	for rows.Next() {
		var content string
		if err := rows.Scan(&content); err != nil {
			return "", err
		}
		if strings.TrimSpace(content) != "" {
			parts = append(parts, content)
		}
	}
	return strings.Join(parts, "\n\n"), rows.Err()
}

// GetConversationSkillIDs returns the IDs of skills enabled for a conversation.
func GetConversationSkillIDs(conversationID uint) ([]uint, error) {
	rows, err := models.DB.Query(
		"SELECT skill_id FROM conversation_skills WHERE conversation_id = ? AND enabled = 1",
		conversationID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []uint
	for rows.Next() {
		var id uint
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// SetConversationSkill enables or disables a skill for a conversation.
func SetConversationSkill(conversationID, skillID uint, enabled bool) error {
	val := 0
	if enabled {
		val = 1
	}
	_, err := models.DB.Exec(`
		INSERT INTO conversation_skills (conversation_id, skill_id, enabled)
		VALUES (?, ?, ?)
		ON CONFLICT(conversation_id, skill_id) DO UPDATE SET enabled = excluded.enabled
	`, conversationID, skillID, val)
	return err
}