package services

import (
	"fmt"
	"strconv"

	"YourQL/pkg/models"
)

// GetDiscussionDefaults returns the user's configured defaults for new
// discussions. Returns nil pointers for any setting that hasn't been
// configured, which means "use the system default."
func GetDiscussionDefaults() (*models.DiscussionDefaults, error) {
	rows, err := models.DB.Query("SELECT key, value FROM discussion_defaults")
	if err != nil {
		return nil, fmt.Errorf("failed to query discussion defaults: %w", err)
	}
	defer rows.Close()

	d := &models.DiscussionDefaults{}
	raw := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			continue
		}
		raw[k] = v
	}

	if v, ok := raw["llm_provider_id"]; ok && v != "" {
		if id, err := strconv.ParseUint(v, 10, 64); err == nil {
			uid := uint(id)
			d.LLMProviderID = &uid
		}
	}
	if v, ok := raw["data_source_id"]; ok && v != "" {
		if id, err := strconv.ParseUint(v, 10, 64); err == nil {
			uid := uint(id)
			d.DataSourceID = &uid
		}
	}
	if v, ok := raw["max_context_messages"]; ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			d.MaxContextMessages = &n
		}
	}
	if v, ok := raw["max_messages"]; ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			d.MaxMessages = &n
		}
	}
	if v, ok := raw["summarize"]; ok {
		b := v == "true"
		d.Summarize = &b
	}
	if v, ok := raw["viz_enabled"]; ok {
		b := v == "true"
		d.VizEnabled = &b
	}
	if v, ok := raw["tech_details"]; ok {
		b := v == "true"
		d.TechDetails = &b
	}
	if v, ok := raw["context_details"]; ok {
		b := v == "true"
		d.ContextDetails = &b
	}
	if v, ok := raw["streaming_enabled"]; ok {
		b := v == "true"
		d.StreamingEnabled = &b
	}

	return d, nil
}

// SetDiscussionDefault upserts a single default setting. An empty value
// removes the key (reverts to system default for that field).
func SetDiscussionDefault(key, value string) error {
	if value == "" {
		_, err := models.DB.Exec("DELETE FROM discussion_defaults WHERE key = ?", key)
		return err
	}
	_, err := models.DB.Exec(
		"INSERT INTO discussion_defaults (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
		key, value,
	)
	if err != nil {
		return fmt.Errorf("failed to set discussion default %s: %w", key, err)
	}
	return nil
}
