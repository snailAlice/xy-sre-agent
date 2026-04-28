package alertmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"xy-sre-agent/internal/config"
)

type Client interface {
	GetActiveAlerts(ctx context.Context) ([]Alert, error)
}

type HTTPClient struct {
	cfg    config.AlertmanagerConfig
	client *http.Client
	logger *slog.Logger
}

func NewClient(cfg config.AlertmanagerConfig, client *http.Client, logger *slog.Logger) *HTTPClient {
	return &HTTPClient{cfg: cfg, client: client, logger: logger}
}

func (c *HTTPClient) GetActiveAlerts(ctx context.Context) ([]Alert, error) {
	if c.cfg.URL == "" {
		return nil, fmt.Errorf("alertmanager.url is empty")
	}
	base, err := url.Parse(strings.TrimRight(c.cfg.URL, "/") + "/api/v2/alerts")
	if err != nil {
		return nil, err
	}
	q := base.Query()
	q.Set("active", "true")
	base.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String(), nil)
	if err != nil {
		return nil, err
	}
	if c.cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("alertmanager returned status %d", resp.StatusCode)
	}

	var alerts []Alert
	if err := json.NewDecoder(resp.Body).Decode(&alerts); err != nil {
		return nil, err
	}
	return alerts, nil
}
