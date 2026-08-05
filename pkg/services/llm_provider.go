package services

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"YourQL/pkg/models"
)

func CreateLLMProvider(name, provider, model, baseURL, apiKey string, isDefault bool, configJSON string, maxTokens *int) (*models.LLMProvider, error) {
	now := time.Now().UTC()
	isActive := true

	var configArg interface{}
	if configJSON == "" {
		configArg = nil
	} else {
		configArg = configJSON
	}

	var maxTokensArg interface{}
	if maxTokens != nil {
		maxTokensArg = *maxTokens
	} else {
		maxTokensArg = 2000
	}

	result, err := models.DB.Exec(
		"INSERT INTO llm_providers (name, provider, model, base_url, api_key, is_default, is_active, config, max_tokens, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		name, provider, model, baseURL, apiKey, isDefault, isActive, configArg, maxTokensArg, now, now,
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
	var maxTokensNull, modelMaxTokensNull, contextWindowNull sql.NullInt64
	var configNull []byte
	err := models.DB.QueryRow(
		"SELECT id, name, provider, model, base_url, api_key, is_default, is_active, max_tokens, model_max_tokens, context_window, config, created_at, updated_at FROM llm_providers WHERE id = ? LIMIT 1",
		id,
	).Scan(
		&p.ID, &p.Name, &p.Provider, &modelNull, &baseURLNull, &apiKeyNull,
		&p.IsDefault, &p.IsActive, &maxTokensNull, &modelMaxTokensNull, &contextWindowNull, &configNull, &p.CreatedAt, &p.UpdatedAt,
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
	if maxTokensNull.Valid {
		v := int(maxTokensNull.Int64)
		p.MaxTokens = &v
	}
	if modelMaxTokensNull.Valid {
		v := int(modelMaxTokensNull.Int64)
		p.ModelMaxTokens = &v
	}
	if contextWindowNull.Valid {
		v := int(contextWindowNull.Int64)
		p.ContextWindow = &v
	}
	return &p, nil
}

func ListLLMProvidersByWorkspace() ([]*models.LLMProvider, error) {
	rows, err := models.DB.Query(
		"SELECT id, name, provider, model, base_url, api_key, is_default, is_active, max_tokens, model_max_tokens, context_window, config, created_at, updated_at FROM llm_providers ORDER BY is_default DESC, created_at DESC",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list LLM providers: %w", err)
	}
	defer rows.Close()

	providers := make([]*models.LLMProvider, 0)
	for rows.Next() {
		var p models.LLMProvider
		var modelNull, baseURLNull, apiKeyNull sql.NullString
		var maxTokensNull, modelMaxTokensNull, contextWindowNull sql.NullInt64
		var configNull []byte
		err := rows.Scan(
			&p.ID, &p.Name, &p.Provider, &modelNull, &baseURLNull, &apiKeyNull,
			&p.IsDefault, &p.IsActive, &maxTokensNull, &modelMaxTokensNull, &contextWindowNull, &configNull, &p.CreatedAt, &p.UpdatedAt,
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
		if maxTokensNull.Valid {
			v := int(maxTokensNull.Int64)
			p.MaxTokens = &v
		}
		if modelMaxTokensNull.Valid {
			v := int(modelMaxTokensNull.Int64)
			p.ModelMaxTokens = &v
		}
		if contextWindowNull.Valid {
			v := int(contextWindowNull.Int64)
			p.ContextWindow = &v
		}
		providers = append(providers, &p)
	}
	return providers, nil
}

func UpdateLLMProvider(id uint, name *string, model *string, baseURL *string, apiKey *string, configJSON *string, maxTokens *int) (*models.LLMProvider, error) {
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
	if maxTokens != nil {
		updates = append(updates, "max_tokens = ?")
		args = append(args, *maxTokens)
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

// DetectModelMaxTokens attempts to detect a model's maximum output tokens
// from the provider's API. Returns nil,nil when detection is not supported
// or fails.
func DetectModelMaxTokens(provider *models.LLMProvider) (*int, error) {
	switch provider.Provider {
	case "openai":
		return detectOpenAIModelMaxTokens(provider)
	case "ollama":
		return detectOllamaModelMaxTokens(provider)
	default:
		// anthropic and local do not support detection
		return nil, nil
	}
}

// UpdateModelMaxTokens persists the detected model max tokens for a provider.
func UpdateModelMaxTokens(providerID uint, modelMaxTokens *int) error {
	var arg interface{}
	if modelMaxTokens != nil {
		arg = *modelMaxTokens
	} else {
		arg = nil
	}
	_, err := models.DB.Exec("UPDATE llm_providers SET model_max_tokens = ? WHERE id = ?", arg, providerID)
	return err
}

func detectOpenAIModelMaxTokens(provider *models.LLMProvider) (*int, error) {
	baseURL := "https://api.openai.com/v1"
	if provider.BaseURL != nil && *provider.BaseURL != "" {
		baseURL = *provider.BaseURL
	}
	modelName := ""
	if provider.Model != nil {
		modelName = *provider.Model
	}
	if modelName == "" {
		return nil, nil
	}

	apiKey := ""
	if provider.APIKey != nil {
		apiKey = *provider.APIKey
	}

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", baseURL+"/models/"+modelName, nil)
	if err != nil {
		return nil, nil
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil
	}

	// Try to extract max_tokens, context_length, or context_window
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, nil
	}

	// Check common field names
	for _, key := range []string{"max_tokens", "context_length", "context_window", "max_output_tokens"} {
		if v, ok := result[key]; ok {
			switch val := v.(type) {
			case float64:
				n := int(val)
				return &n, nil
			case int:
				return &val, nil
			}
		}
	}

	return nil, nil
}

func detectOllamaModelMaxTokens(provider *models.LLMProvider) (*int, error) {
	baseURL := "http://localhost:11434"
	if provider.BaseURL != nil && *provider.BaseURL != "" {
		baseURL = *provider.BaseURL
	}
	modelName := ""
	if provider.Model != nil {
		modelName = *provider.Model
	}
	if modelName == "" {
		return nil, nil
	}

	reqBody, _ := json.Marshal(map[string]string{"name": modelName})
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(baseURL+"/api/show", "application/json", strings.NewReader(string(reqBody)))
	if err != nil {
		return nil, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, nil
	}

	// Check model_info for context parameters
	if modelInfo, ok := result["model_info"].(map[string]interface{}); ok {
		for _, key := range []string{"num_ctx", "context_length", "max_tokens"} {
			if v, ok := modelInfo[key]; ok {
				switch val := v.(type) {
				case float64:
					n := int(val)
					return &n, nil
				case int:
					return &val, nil
				}
			}
		}
	}

	return nil, nil
}
