package tools

import (
	"context"
	"encoding/json"
	"time"

	"xy-sre-agent/internal/alertmanager"
	"xy-sre-agent/internal/llm"
)

type GetActiveAlertsTool struct {
	client alertmanager.Client
}

func NewGetActiveAlertsTool(client alertmanager.Client) *GetActiveAlertsTool {
	return &GetActiveAlertsTool{client: client}
}

func (t *GetActiveAlertsTool) Name() string { return "get_active_alerts" }
func (t *GetActiveAlertsTool) Description() string {
	return "从 Alertmanager API 查询当前 active 告警"
}
func (t *GetActiveAlertsTool) Definition() llm.ToolSpec {
	return llm.ToolSpec{
		Type: "function",
		Function: llm.ToolFunctionSpec{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  schema(map[string]any{}, nil),
		},
	}
}

func (t *GetActiveAlertsTool) Execute(ctx context.Context, _ json.RawMessage) (Result, error) {
	start := time.Now()
	alerts, err := t.client.GetActiveAlerts(ctx)
	raw, _ := json.MarshalIndent(alerts, "", "  ")
	return Result{
		Name:      t.Name(),
		Summary:   "当前 active 告警",
		Output:    string(raw),
		StartedAt: start,
		Duration:  time.Since(start).String(),
		Metadata:  map[string]any{"count": len(alerts)},
	}, err
}
