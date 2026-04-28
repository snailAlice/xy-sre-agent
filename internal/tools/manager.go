package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"

	"xy-sre-agent/internal/llm"
)

type Manager struct {
	tools  map[string]Tool
	logger *slog.Logger
}

func NewManager(logger *slog.Logger) *Manager {
	return &Manager{tools: make(map[string]Tool), logger: logger}
}

func (m *Manager) Register(tool Tool) {
	m.tools[tool.Name()] = tool
}

func (m *Manager) Execute(ctx context.Context, name string, args any) (Result, error) {
	tool, ok := m.tools[name]
	if !ok {
		return Result{}, fmt.Errorf("tool %q not registered", name)
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return Result{}, err
	}
	m.logger.Info("execute tool", "tool", name)
	return tool.Execute(ctx, raw)
}

func (m *Manager) Definitions() []llm.ToolSpec {
	out := make([]llm.ToolSpec, 0, len(m.tools))
	names := m.Names()
	for _, name := range names {
		out = append(out, m.tools[name].Definition())
	}
	return out
}

func (m *Manager) Names() []string {
	names := make([]string, 0, len(m.tools))
	for name := range m.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
