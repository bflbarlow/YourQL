package services

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"YourQL/pkg/models"
)

// CreateLLMProvider creates a new LLM provider configuration for a workspace.
func CreateLLMProvider(workspaceID, createdBy uint, name, provider, model, baseURL string, apiKey string, isDefault bool, configJSON string) (*models.LLMProvider, error) {
	isMember, err := IsWorkspaceMember(workspaceID, createdBy)
	if err != nil {
		return nil, fmt.Errorf("failed to check workspace membership: %w", err)
	}
	if !isMember {
		return nil, errors.New("user is not a member of this workspace")
	}

	hasPermission, err := CheckPermission(workspaceID, createdBy, "can_manage_llm")
	if err != nil {
		return nil, err
	}
	if !hasPermission {
		return nil, errors.New("insufficient permissions to manage LLM providers")
	}

	now := time.Now().UTC()
	isActive := true

	// Handle empty config as NULL for JSON column
	var configArg interface{}
	if configJSON == "" {
		configArg = nil
	} else {
		configArg = configJSON
	}

	result, err := models.DB.Exec(
		"INSERT INTO llm_providers (workspace_id, name, provider, model, base_url, api_key, is_default, is_active, config, created_by, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		workspaceID, name, provider, model, baseURL, apiKey, isDefault, isActive, configArg, createdBy, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create LLM provider: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get LLM provider ID: %w", err)
	}

	providerName := &models.LLMProvider{
		ID:          uint(id),
		WorkspaceID: workspaceID,
		Name:        name,
		Provider:    provider,
		Model:       &model,
		BaseURL:     &baseURL,
		IsActive:    isActive,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if isDefault {
		if err := setDefaultLLMProvider(workspaceID, uint(id)); err != nil {
			return nil, err
		}
	}

	return providerName, nil
}

// GetLLMProviderByID retrieves an LLM provider by ID.
func GetLLMProviderByID(id uint) (*models.LLMProvider, error) {
	var p models.LLMProvider
	var modelNull, baseURLNull, apiKeyNull sql.NullString
	var configNull []byte
	var createdByNull sql.NullInt64
	err := models.DB.QueryRow(
		"SELECT id, workspace_id, name, provider, model, base_url, api_key, is_default, is_active, config, created_by, created_at, updated_at FROM llm_providers WHERE id = ? LIMIT 1",
		id,
	).Scan(
		&p.ID, &p.WorkspaceID, &p.Name, &p.Provider, &modelNull, &baseURLNull, &apiKeyNull,
		&p.IsDefault, &p.IsActive, &configNull, &createdByNull, &p.CreatedAt, &p.UpdatedAt,
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
	if createdByNull.Valid {
		cb := uint(createdByNull.Int64)
		p.CreatedBy = &cb
	}
	return &p, nil
}

// ListLLMProvidersByWorkspace lists all LLM providers for a workspace.
func ListLLMProvidersByWorkspace(workspaceID uint) ([]*models.LLMProvider, error) {
	rows, err := models.DB.Query(
		"SELECT id, workspace_id, name, provider, model, base_url, api_key, is_default, is_active, config, created_by, created_at, updated_at FROM llm_providers WHERE workspace_id = ? ORDER BY is_default DESC, created_at DESC",
		workspaceID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list LLM providers: %w", err)
	}
	defer rows.Close()

	var providers []*models.LLMProvider
	for rows.Next() {
		var p models.LLMProvider
		var modelNull, baseURLNull, apiKeyNull sql.NullString
		var configNull []byte
		var createdByNull sql.NullInt64
		err := rows.Scan(
			&p.ID, &p.WorkspaceID, &p.Name, &p.Provider, &modelNull, &baseURLNull, &apiKeyNull,
			&p.IsDefault, &p.IsActive, &configNull, &createdByNull, &p.CreatedAt, &p.UpdatedAt,
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
		if createdByNull.Valid {
			cb := uint(createdByNull.Int64)
			p.CreatedBy = &cb
		}
		providers = append(providers, &p)
	}
	return providers, nil
}

// UpdateLLMProvider updates an LLM provider's configuration.
func UpdateLLMProvider(id uint, name *string, model *string, baseURL *string, apiKey *string, configJSON *string) (*models.LLMProvider, error) {
	p, err := GetLLMProviderByID(id)
	if err != nil {
		return nil, err
	}

	var createdBy uint
	if p.CreatedBy != nil {
		createdBy = *p.CreatedBy
	}
	isMember, err := IsWorkspaceMember(p.WorkspaceID, createdBy)
	if err != nil {
		return nil, err
	}
	if !isMember {
		return nil, errors.New("user is not a member of this workspace")
	}

	hasPermission, err := CheckPermission(p.WorkspaceID, createdBy, "can_manage_llm")
	if err != nil {
		return nil, err
	}
	if !hasPermission {
		return nil, errors.New("insufficient permissions to update LLM provider")
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

	query := fmt.Sprintf("UPDATE llm_providers SET %s, updated_at = CURRENT_TIMESTAMP WHERE id = ?", strings.Join(updates, ", "))
	_, err = models.DB.Exec(query, append(args, id)...)
	if err != nil {
		return nil, fmt.Errorf("failed to update LLM provider: %w", err)
	}

	return GetLLMProviderByID(id)
}

// DeleteLLMProvider deletes an LLM provider.
func DeleteLLMProvider(id uint) error {
	p, err := GetLLMProviderByID(id)
	if err != nil {
		return err
	}

	// Check if this provider is set as default
	if p.IsDefault {
		return errors.New("cannot delete default LLM provider; set another as default first")
	}

	// Check if any active conversations use this provider
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

// SetDefaultLLMProvider sets an LLM provider as the default for a workspace.
func SetDefaultLLMProvider(workspaceID, providerID uint) error {
	// Verify the provider belongs to this workspace
	var exists bool
	err := models.DB.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM llm_providers WHERE id = ? AND workspace_id = ?)",
		providerID, workspaceID,
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to verify provider: %w", err)
	}
	if !exists {
		return errors.New("provider not found in workspace")
	}

	// Unset all defaults in this workspace
	_, err = models.DB.Exec(
		"UPDATE llm_providers SET is_default = 0 WHERE workspace_id = ?",
		workspaceID,
	)
	if err != nil {
		return fmt.Errorf("failed to unset previous defaults: %w", err)
	}

	// Set the new default
	_, err = models.DB.Exec(
		"UPDATE llm_providers SET is_default = 1 WHERE id = ?",
		providerID,
	)
	if err != nil {
		return fmt.Errorf("failed to set default: %w", err)
	}

	return nil
}

// GetDefaultLLMProvider retrieves the default LLM provider for a workspace.
func GetDefaultLLMProvider(workspaceID uint) (*models.LLMProvider, error) {
	var p models.LLMProvider
	var modelNull, baseURLNull, apiKeyNull sql.NullString
	var configNull []byte
	var createdByNull sql.NullInt64
	err := models.DB.QueryRow(
		"SELECT id, workspace_id, name, provider, model, base_url, api_key, is_default, is_active, config, created_by, created_at, updated_at FROM llm_providers WHERE workspace_id = ? AND is_default = 1 LIMIT 1",
		workspaceID,
	).Scan(
		&p.ID, &p.WorkspaceID, &p.Name, &p.Provider, &modelNull, &baseURLNull, &apiKeyNull,
		&p.IsDefault, &p.IsActive, &configNull, &createdByNull, &p.CreatedAt, &p.UpdatedAt,
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
	if createdByNull.Valid {
		cb := uint(createdByNull.Int64)
		p.CreatedBy = &cb
	}
	return &p, nil
}

// TestLLMProvider tests if an LLM provider configuration is valid and reachable.
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
	case "mock":
		return "Mock provider - no API call made", nil
	default:
		return "", fmt.Errorf("unsupported provider type: %s", provider.Provider)
	}
}

// setDefaultLLMProvider is an internal helper to set default without workspace sync.
func setDefaultLLMProvider(workspaceID, providerID uint) error {
	_, err := models.DB.Exec(
		"UPDATE llm_providers SET is_default = 0 WHERE workspace_id = ?",
		workspaceID,
	)
	if err != nil {
		return err
	}
	_, err = models.DB.Exec(
		"UPDATE llm_providers SET is_default = 1 WHERE id = ?",
		providerID,
	)
	return err
}
