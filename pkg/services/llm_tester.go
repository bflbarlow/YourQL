package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

// testOpenAIConnection tests the OpenAI API connection.
func testOpenAIConnection(apiKey, model, baseURL *string) (string, error) {
	baseURLStr := "https://api.openai.com/v1"
	if baseURL != nil && *baseURL != "" {
		baseURLStr = *baseURL
	}

	localEndpoint := baseURLStr != "https://api.openai.com/v1"

	modelName := "gpt-3.5-turbo"
	if model != nil && *model != "" {
		modelName = *model
	} else if localEndpoint {
		modelName = "gemma3:4b"
	}

	reqBody := map[string]interface{}{
		"model":      modelName,
		"messages":   []map[string]string{{"role": "user", "content": "Hello"}},
		"max_tokens": 5,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", baseURLStr+"/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}
	if apiKey != nil && *apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+*apiKey)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("OpenAI API error: status %d", resp.StatusCode)
	}

	if localEndpoint {
		return fmt.Sprintf("Local model connection successful (model: %s)", modelName), nil
	}
	return fmt.Sprintf("OpenAI API connection successful (model: %s)", modelName), nil
}

// testAnthropicConnection tests the Anthropic API connection.
func testAnthropicConnection(apiKey, model, baseURL *string) (string, error) {
	if apiKey == nil || *apiKey == "" {
		return "", fmt.Errorf("API key is required")
	}

	baseURLStr := "https://api.anthropic.com"
	if baseURL != nil && *baseURL != "" {
		baseURLStr = *baseURL
	}

	reqBody := map[string]interface{}{
		"model":      "claude-3-haiku-20240307",
		"max_tokens": 1,
		"messages":   []map[string]string{{"role": "user", "content": "Hello"}},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", baseURLStr+"/v1/messages", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}
	req.Header.Set("x-api-key", *apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	modelName := "claude-3-haiku-20240307"
	if model != nil && *model != "" {
		modelName = *model
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("Anthropic API error: status %d", resp.StatusCode)
	}

	return fmt.Sprintf("Anthropic Claude API connection successful (model: %s)", modelName), nil
}

// testOllamaConnection tests the Ollama API connection.
func testOllamaConnection(baseURL, model *string) (string, error) {
	baseURLStr := "http://localhost:11434"
	if baseURL != nil && *baseURL != "" {
		baseURLStr = removeTrailingSlash(*baseURL)
	}

	reqBody := map[string]interface{}{
		"model":  "llama2",
		"stream": false,
		"messages": []map[string]string{
			{"role": "user", "content": "Hello"},
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", baseURLStr+"/api/chat", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	modelName := "llama2"
	if model != nil && *model != "" {
		modelName = *model
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("Ollama API error: status %d", resp.StatusCode)
	}

	return fmt.Sprintf("Ollama API connection successful (model: %s)", modelName), nil
}

// testLocalConnection tests the local model HTTP endpoint.
func testLocalConnection(baseURL, model *string) (string, error) {
	if baseURL == nil || *baseURL == "" {
		return "", fmt.Errorf("base_url is required for local provider (use an OpenAI-compatible HTTP endpoint)")
	}
	url := removeTrailingSlash(*baseURL)
	modelName := "local-model"
	if model != nil && *model != "" {
		modelName = *model
	}
	reqBody := map[string]interface{}{
		"model":  modelName,
		"stream": false,
		"messages": []map[string]string{
			{"role": "user", "content": "Hello"},
		},
	}
	jsonData, _ := json.Marshal(reqBody)
	resp, err := http.Post(url+"/v1/chat/completions", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("local model API error: status %d", resp.StatusCode)
	}
	return fmt.Sprintf("Local model connection successful (model: %s)", modelName), nil
}
