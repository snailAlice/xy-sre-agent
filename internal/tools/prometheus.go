package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"xy-sre-agent/internal/config"
	"xy-sre-agent/internal/llm"
)

type PromQueryTool struct {
	cfg    config.PrometheusConfig
	client *http.Client
	logger *slog.Logger
}

func NewPromQueryTool(cfg config.PrometheusConfig, client *http.Client, logger *slog.Logger) *PromQueryTool {
	return &PromQueryTool{cfg: cfg, client: client, logger: logger}
}

func (t *PromQueryTool) Name() string { return "prom_query" }
func (t *PromQueryTool) Description() string {
	return "执行 Prometheus instant query，用于查询 CPU、内存、QPS、错误率、磁盘使用率等指标"
}
func (t *PromQueryTool) Definition() llm.ToolSpec {
	return llm.ToolSpec{Type: "function", Function: llm.ToolFunctionSpec{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: schema(map[string]any{
			"promql": map[string]any{"type": "string", "description": "PromQL 表达式"},
		}, []string{"promql"}),
	}}
}
func (t *PromQueryTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	start := time.Now()
	var input struct {
		PromQL string `json:"promql"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(input.PromQL) == "" {
		return Result{}, fmt.Errorf("promql is required")
	}
	if t.cfg.URL == "" {
		return Result{}, fmt.Errorf("prometheus.url is empty")
	}

	u, err := url.Parse(strings.TrimRight(t.cfg.URL, "/") + "/api/v1/query")
	if err != nil {
		return Result{}, err
	}
	q := u.Query()
	q.Set("query", input.PromQL)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Result{}, err
	}
	if t.cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+t.cfg.Token)
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return Result{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, fmt.Errorf("prometheus status %d: %s", resp.StatusCode, string(body))
	}
	return Result{
		Name:      t.Name(),
		Summary:   "Prometheus 查询结果",
		Output:    string(body),
		StartedAt: start,
		Duration:  time.Since(start).String(),
		Metadata:  map[string]any{"promql": input.PromQL},
	}, nil
}
