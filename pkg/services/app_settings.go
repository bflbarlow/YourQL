package services

import (
	"fmt"

	"YourQL/pkg/models"
)

// GetAppSetting returns the value for a settings key, or empty string if not set.
func GetAppSetting(key string) (string, error) {
	var value string
	err := models.DB.QueryRow(
		"SELECT value FROM app_settings WHERE key = ?", key,
	).Scan(&value)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return "", nil
		}
		return "", fmt.Errorf("failed to get app setting %s: %w", key, err)
	}
	return value, nil
}

// SetAppSetting upserts a key-value setting. An empty value deletes the key.
func SetAppSetting(key, value string) error {
	if value == "" {
		_, err := models.DB.Exec("DELETE FROM app_settings WHERE key = ?", key)
		return err
	}
	_, err := models.DB.Exec(
		"INSERT INTO app_settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
		key, value,
	)
	if err != nil {
		return fmt.Errorf("failed to set app setting %s: %w", key, err)
	}
	return nil
}

// GetAllAppSettings returns all settings as a map.
func GetAllAppSettings() (map[string]string, error) {
	rows, err := models.DB.Query("SELECT key, value FROM app_settings")
	if err != nil {
		return nil, fmt.Errorf("failed to query app settings: %w", err)
	}
	defer rows.Close()

	settings := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			continue
		}
		settings[k] = v
	}
	return settings, nil
}