package services

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"YourQL/pkg/models"
)

func CreateLLMProvider(name, provider, model, baseURL, apiKey string, isDefault bool, configJSON string) (*models.LLMProvider, error) {
	now := time.Now().UTC()
	isActive := true

	var configArg interface{}
	if configJSON == "" {
		configArg = nil
	} else {
		configArg = configJSON
	}

	result, err := models.DB.Exec(
		"INSERT INTO llm_providers (name, provider, model, base_url, api_key, is_default, is_active, config, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		name, provider, model, baseURL, apiKey, isDefault, isActive, configArg, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create LLM provider: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get LLM provider ID: %w", err)
	}

	p := &models.LLMProvider{
		ID:       uint(id),
		Name:     name,
		Provider: provider,
		Model:    &model,
		BaseURL:  &baseURL,
		IsActive: isActive,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if isDefault {
		if err := setDefaultLLMProvider(uint(id)); err != nil {
			return nil, err
		}
	}

	return p, nil
}

func GetLLMProviderByID(id uint) (*models.LLMProvider, error) {
	var p models.LLMProvider
	var modelNull, baseURLNull, apiKeyNull sql.NullString
	var configNull []byte
	err := models.DB.QueryRow(
		"SELECT id, name, provider, model, base_url, api_key, is_default, is_active, config, created_at, updated_at FROM llm_providers WHERE id = ? LIMIT 1",
		id,
	).Scan(
		&p.ID, &p.Name, &p.Provider, &modelNull, &baseURLNull, &apiKeyNull,
		&p.IsDefault, &p.IsActive, &configNull, &p.CreatedAt, &p.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, errors.New("LLM provider not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get LLM provider: %w", err)
	}
	if modelNull.Valid {
		p.Model = &modelNull.String
	}
	if baseURLNull.Valid {
		p.BaseURL = &baseURLNull.String
	}
	if apiKeyNull.Valid {
		p.APIKey = &apiKeyNull.String
	}
	if len(configNull) > 0 {
		s := string(configNull)
		p.Config = &s
	}
	return &p, nil
}

func ListLLMProvidersByWorkspace() ([]*models.LLMProvider, error) {
	rows, err := models.DB.Query(
		"SELECT id, name, provider, model, base_url, api_key, is_default, is_active, config, created_at, updated_at FROM llm_providers ORDER BY is_default DESC, created_at DESC",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list LLM providers: %w", err)
	}
	defer rows.Close()

	providers := make([]*models.LLMProvider, 0)
	for rows.Next() {
		var p models.LLMProvider
		var modelNull, baseURLNull, apiKeyNull sql.NullString
		var configNull []byte
		err := rows.Scan(
			&p.ID, &p.Name, &p.Provider, &modelNull, &baseURLNull, &apiKeyNull,
			&p.IsDefault, &p.IsActive, &configNull, &p.CreatedAt, &p.UpdatedAt,
		)
		if err != nil {
			continue
		}
		if modelNull.Valid {
			p.Model = &modelNull.String
		}
		if baseURLNull.Valid {
			p.BaseURL = &baseURLNull.String
		}
		if apiKeyNull.Valid {
			p.APIKey = &apiKeyNull.String
		}
		if len(configNull) > 0 {
			s := string(configNull)
			p.Config = &s
		}
		providers = append(providers, &p)
	}
	return providers, nil
}

func UpdateLLMProvider(id uint, name *string, model *string, baseURL *string, apiKey *string, configJSON *string) (*models.LLMProvider, error) {
	p, err := GetLLMProviderByID(id)
	if err != nil {
		return nil, err
	}

	updates := make([]string, 0)
	args := []interface{}{}

	if name != nil {
		updates = append(updates, "name = ?")
		args = append(args, *name)
	}
	if model != nil {
		updates = append(updates, "model = ?")
		args = append(args, *model)
	}
	if baseURL != nil {
		updates = append(updates, "base_url = ?")
		args = append(args, *baseURL)
	}
	if apiKey != nil {
		updates = append(updates, "api_key = ?")
		args = append(args, *apiKey)
	}
	if configJSON != nil {
		updates = append(updates, "config = ?")
		if *configJSON == "" {
			args = append(args, nil)
		} else {
			args = append(args, *configJSON)
		}
	}

	if len(updates) == 0 {
		return p, nil
	}

	args = append(args, id)
	query := fmt.Sprintf("UPDATE llm_providers SET %s, updated_at = CURRENT_TIMESTAMP WHERE id = ?", strings.Join(updates, ", "))
	_, err = models.DB.Exec(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to update LLM provider: %w", err)
	}

	return GetLLMProviderByID(id)
}

func DeleteLLMProvider(id uint) error {
	p, err := GetLLMProviderByID(id)
	if err != nil {
		return err
	}

	if p.IsDefault {
		return errors.New("cannot delete default LLM provider; set another as default first")
	}

	var count int
	err = models.DB.QueryRow(
		"SELECT COUNT(*) FROM conversations WHERE llm_provider_id = ? AND status = 'active'",
		id,
	).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check conversation references: %w", err)
	}
	if count > 0 {
		return fmt.Errorf("cannot delete: %d active conversations reference this provider", count)
	}

	_, err = models.DB.Exec("DELETE FROM llm_providers WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete LLM provider: %w", err)
	}
	return nil
}

func SetDefaultLLMProvider(providerID uint) error {
	var exists bool
	err := models.DB.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM llm_providers WHERE id = ?)",
		providerID,
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to verify provider: %w", err)
	}
	if !exists {
		return errors.New("provider not found")
	}

	_, err = models.DB.Exec("UPDATE llm_providers SET is_default = 0")
	if err != nil {
		return fmt.Errorf("failed to unset previous defaults: %w", err)
	}

	_, err = models.DB.Exec("UPDATE llm_providers SET is_default = 1 WHERE id = ?", providerID)
	if err != nil {
		return fmt.Errorf("failed to set default: %w", err)
	}

	return nil
}

func GetDefaultLLMProvider() (*models.LLMProvider, error) {
	var p models.LLMProvider
	var modelNull, baseURLNull, apiKeyNull sql.NullString
	var configNull []byte
	err := models.DB.QueryRow(
		"SELECT id, name, provider, model, base_url, api_key, is_default, is_active, config, created_at, updated_at FROM llm_providers WHERE is_default = 1 LIMIT 1",
	).Scan(
		&p.ID, &p.Name, &p.Provider, &modelNull, &baseURLNull, &apiKeyNull,
		&p.IsDefault, &p.IsActive, &configNull, &p.CreatedAt, &p.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get default LLM provider: %w", err)
	}
	if modelNull.Valid {
		p.Model = &modelNull.String
	}
	if baseURLNull.Valid {
		p.BaseURL = &baseURLNull.String
	}
	if apiKeyNull.Valid {
		p.APIKey = &apiKeyNull.String
	}
	if len(configNull) > 0 {
		s := string(configNull)
		p.Config = &s
	}
	return &p, nil
}

func TestLLMProvider(provider *models.LLMProvider) (string, error) {
	switch provider.Provider {
	case "openai":
		return testOpenAIConnection(provider.APIKey, provider.Model, provider.BaseURL)
	case "anthropic":
		return testAnthropicConnection(provider.APIKey, provider.Model, provider.BaseURL)
	case "ollama":
		return testOllamaConnection(provider.BaseURL, provider.Model)
	case "local":
		return testLocalConnection(provider.BaseURL, provider.Model)
	default:
		return "", fmt.Errorf("unsupported provider type: %s", provider.Provider)
	}
}

func setDefaultLLMProvider(providerID uint) error {
	_, err := models.DB.Exec("UPDATE llm_providers SET is_default = 0")
	if err != nil {
		return err
	}
	_, err = models.DB.Exec("UPDATE llm_providers SET is_default = 1 WHERE id = ?", providerID)
	return err
}
