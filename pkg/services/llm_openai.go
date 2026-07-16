package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"YourQL/pkg/models"
)

// OpenAIClient implements LLMClient for OpenAI's API.
type OpenAIClient struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
}

func NewOpenAIClient(provider *models.LLMProvider) (LLMClient, error) {
	model := "gpt-3.5-turbo"
	if provider.Model != nil && *provider.Model != "" {
		model = *provider.Model
	}

	baseURL := "https://api.openai.com/v1"
	if provider.BaseURL != nil && *provider.BaseURL != "" {
		baseURL = *provider.BaseURL
	}

	localEndpoint := baseURL != "https://api.openai.com/v1"
	var apiKey string
	if provider.APIKey != nil {
		apiKey = *provider.APIKey
	}
	if !localEndpoint && apiKey == "" {
		return nil, fmt.Errorf("OpenAI API key is required for https://api.openai.com/v1")
	}

	return &OpenAIClient{baseURL: baseURL, apiKey: apiKey, model: model, httpClient: &http.Client{Timeout: 300 * time.Second}}, nil
}

type openAIChatRequest struct {
	Model            string              `json:"model"`
	Messages         []openAIChatMessage `json:"messages"`
	Stream           bool                `json:"stream,omitempty"`
	MaxTokens        int                 `json:"max_tokens,omitempty"`
	Temperature      float64             `json:"temperature,omitempty"`
	TopP             float64             `json:"top_p,omitempty"`
	FrequencyPenalty float64             `json:"frequency_penalty,omitempty"`
	PresencePenalty  float64             `json:"presence_penalty,omitempty"`
}

type openAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

func (c *OpenAIClient) ChatCompletion(ctx context.Context, messages []ChatMessage) (string, error) {
	content, _, _, err := c.ChatCompletionWithPayload(ctx, messages)
	return content, err
}

func (c *OpenAIClient) ChatCompletionWithPayload(ctx context.Context, messages []ChatMessage) (content, requestJSON, responseJSON string, err error) {
	openAIMessages := make([]openAIChatMessage, len(messages))
	for i, msg := range messages {
		openAIMessages[i] = openAIChatMessage{Role: msg.Role, Content: msg.Content}
	}

	reqBody := openAIChatRequest{
		Model:       c.model,
		Messages:    openAIMessages,
		Temperature: 0.1,
		MaxTokens:   2000,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to marshal request: %w", err)
	}

	log.Printf("[OpenAI] Sending request to %s/chat/completions: %s", c.baseURL, string(jsonData))

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", "", "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", "", "", fmt.Errorf("OpenAI API returned status %d: %s", resp.StatusCode, string(body))
	}

	var response openAIChatResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return "", "", "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if len(response.Choices) == 0 {
		var errInfo struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if e := json.Unmarshal(body, &errInfo); e == nil && errInfo.Error.Message != "" {
			if strings.Contains(strings.ToLower(errInfo.Error.Message), "endpoint") || strings.Contains(strings.ToLower(errInfo.Error.Message), "unexpected") {
				return "", "", "", fmt.Errorf("API endpoint error: %s. For OpenAI-compatible local APIs, the base URL should point to the server root (e.g., http://localhost:1234), NOT include /v1.", errInfo.Error.Message)
			}
			return "", "", "", fmt.Errorf("API error: %s", errInfo.Error.Message)
		}
		rawBody := string(body)
		if len(rawBody) > 500 {
			rawBody = rawBody[:500] + "..."
		}
		return "", "", "", fmt.Errorf("API returned empty response (no choices). Raw: %s", rawBody)
	}

	return response.Choices[0].Message.Content, string(jsonData), string(body), nil
}
