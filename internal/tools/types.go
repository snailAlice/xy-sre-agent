package tools

import (
	"context"
	"encoding/json"
	"time"

	"xy-sre-agent/internal/llm"
)

type Tool interface {
	Name() string
	Description() string
	Definition() llm.ToolSpec
	Execute(ctx context.Context, raw json.RawMessage) (Result, error)
}

type Result struct {
	Name      string         `json:"name"`
	Summary   string         `json:"summary"`
	Output    string         `json:"output"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	StartedAt time.Time      `json:"started_at"`
	Duration  string         `json:"duration"`
}

func schema(properties map[string]any, required []string) map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           properties,
		"required":             required,
		"additionalProperties": false,
	}
}
