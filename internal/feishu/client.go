package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"xy-sre-agent/internal/config"
)

type Client struct {
	cfg    config.FeishuConfig
	client *http.Client
	logger *slog.Logger

	mu          sync.Mutex
	tenantToken string
	tokenExpire time.Time

	eventMu        sync.Mutex
	processedEvent map[string]time.Time
}

func NewClient(cfg config.FeishuConfig, client *http.Client, logger *slog.Logger) *Client {
	return &Client{
		cfg:            cfg,
		client:         client,
		logger:         logger,
		processedEvent: make(map[string]time.Time),
	}
}

func (c *Client) MarkMessageProcessing(messageID string) bool {
	if messageID == "" {
		return true
	}

	c.eventMu.Lock()
	defer c.eventMu.Unlock()

	now := time.Now()
	for id, seenAt := range c.processedEvent {
		if now.Sub(seenAt) > 10*time.Minute {
			delete(c.processedEvent, id)
		}
	}
	if _, ok := c.processedEvent[messageID]; ok {
		return false
	}
	c.processedEvent[messageID] = now
	return true
}

func (c *Client) SendText(ctx context.Context, text string) error {
	if c.cfg.BotToken == "" {
		c.logger.Warn("feishu.bot_token is empty, skip sending message")
		return nil
	}
	url := c.botWebhookURL()
	payload := map[string]any{
		"msg_type": "text",
		"content":  map[string]string{"text": text},
	}
	return c.postJSON(ctx, url, "", payload, nil)
}

func (c *Client) botWebhookURL() string {
	if strings.HasPrefix(c.cfg.BotToken, "http://") || strings.HasPrefix(c.cfg.BotToken, "https://") {
		return c.cfg.BotToken
	}
	return strings.TrimRight(c.cfg.APIBaseURL, "/") + "/open-apis/bot/v2/hook/" + c.cfg.BotToken
}

func (c *Client) ReplyText(ctx context.Context, messageID, text string) error {
	if messageID == "" {
		return c.SendText(ctx, text)
	}
	token, err := c.tenantAccessToken(ctx)
	if err != nil {
		c.logger.Warn("get tenant token failed, fallback to bot webhook", "error", err)
		return c.SendText(ctx, text)
	}
	url := strings.TrimRight(c.cfg.APIBaseURL, "/") + "/open-apis/im/v1/messages/" + messageID + "/reply"
	content, _ := json.Marshal(map[string]string{"text": text})
	payload := map[string]any{
		"msg_type": "text",
		"content":  string(content),
	}
	return c.postJSON(ctx, url, "Bearer "+token, payload, nil)
}

func (c *Client) tenantAccessToken(ctx context.Context) (string, error) {
	if c.cfg.AppID == "" || c.cfg.AppSecret == "" {
		return "", fmt.Errorf("feishu.app_id/app_secret is empty")
	}
	c.mu.Lock()
	if c.tenantToken != "" && time.Now().Before(c.tokenExpire.Add(-5*time.Minute)) {
		token := c.tenantToken
		c.mu.Unlock()
		return token, nil
	}
	c.mu.Unlock()

	url := strings.TrimRight(c.cfg.APIBaseURL, "/") + "/open-apis/auth/v3/tenant_access_token/internal"
	var resp struct {
		Code              int    `json:"code"`
		Msg               string `json:"msg"`
		TenantAccessToken string `json:"tenant_access_token"`
		Expire            int64  `json:"expire"`
	}
	err := c.postJSON(ctx, url, "", map[string]string{
		"app_id":     c.cfg.AppID,
		"app_secret": c.cfg.AppSecret,
	}, &resp)
	if err != nil {
		return "", err
	}
	if resp.Code != 0 {
		return "", fmt.Errorf("feishu tenant token code=%d msg=%s", resp.Code, resp.Msg)
	}

	c.mu.Lock()
	c.tenantToken = resp.TenantAccessToken
	c.tokenExpire = time.Now().Add(time.Duration(resp.Expire) * time.Second)
	c.mu.Unlock()
	return resp.TenantAccessToken, nil
}

func (c *Client) postJSON(ctx context.Context, url, auth string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("feishu status %d: %s", resp.StatusCode, string(respBody))
	}
	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			return err
		}
	}
	return nil
}
