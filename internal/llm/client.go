package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"xy-sre-agent/internal/config"
)

type Client interface {
	ChatCompletion(ctx context.Context, req ChatCompletionRequest) (string, error)
	Chat(ctx context.Context, req Request) (*Response, error)
}

type HTTPClient struct {
	cfg    config.DeepSeekConfig
	client *http.Client
	logger *slog.Logger
}

type ChatCompletionRequest struct {
	SystemPrompt string
	UserPrompt   string
	Temperature  float64
}

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ToolSpec struct {
	Type     string           `json:"type"`
	Function ToolFunctionSpec `json:"function"`
}

type ToolFunctionSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type Request struct {
	Model       string     `json:"model"`
	Messages    []Message  `json:"messages"`
	Tools       []ToolSpec `json:"tools,omitempty"`
	ToolChoice  any        `json:"tool_choice,omitempty"`
	Temperature float64    `json:"temperature,omitempty"`
}

type Response struct {
	ID      string `json:"id"`
	Choices []struct {
		Index        int     `json:"index"`
		Message      Message `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
	Usage map[string]any `json:"usage,omitempty"`
}

func NewClient(cfg config.DeepSeekConfig, client *http.Client, logger *slog.Logger) *HTTPClient {
	return &HTTPClient{cfg: cfg, client: client, logger: logger}
}

func (c *HTTPClient) ChatCompletion(ctx context.Context, req ChatCompletionRequest) (string, error) {
	resp, err := c.Chat(ctx, Request{
		Messages: []Message{
			{Role: "system", Content: req.SystemPrompt},
			{Role: "user", Content: req.UserPrompt},
		},
		Temperature: req.Temperature,
	})
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("llm response has no choices")
	}
	return strings.TrimSpace(resp.Choices[0].Message.Content), nil
}

func (c *HTTPClient) Chat(ctx context.Context, req Request) (*Response, error) {
	if c.cfg.APIKey == "" {
		return nil, fmt.Errorf("deepseek.api_key is empty")
	}
	if req.Model == "" {
		req.Model = c.cfg.Model
	}

	endpoint := strings.TrimRight(c.cfg.BaseURL, "/") + "/chat/completions"
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	var lastErr error
	attempts := c.cfg.MaxRetries + 1
	for attempt := 1; attempt <= attempts; attempt++ {
		callCtx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
		resp, err := c.do(callCtx, endpoint, body)
		cancel()
		if err == nil {
			return resp, nil
		}
		lastErr = err
		c.logger.Warn("llm call failed", "attempt", attempt, "max_attempts", attempts, "error", err)
		if attempt < attempts {
			time.Sleep(time.Duration(attempt) * 800 * time.Millisecond)
		}
	}
	return nil, lastErr
}

func (c *HTTPClient) do(ctx context.Context, endpoint string, body []byte) (*Response, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)

	httpResp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(httpResp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return nil, fmt.Errorf("llm status %d: %s", httpResp.StatusCode, string(respBody))
	}

	var resp Response
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
