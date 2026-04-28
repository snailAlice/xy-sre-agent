package alertmanager

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"xy-sre-agent/internal/llm"
)

type fakeLLM struct{}

func (fakeLLM) ChatCompletion(context.Context, llm.ChatCompletionRequest) (string, error) {
	return "analysis", nil
}

func (fakeLLM) Chat(context.Context, llm.Request) (*llm.Response, error) {
	return &llm.Response{}, nil
}

type captureNotifier struct {
	count int
}

func (n *captureNotifier) SendText(context.Context, string) error {
	n.count++
	return nil
}

func TestProcessAlertsSkipsSilencedInhibitedAndMutedAlerts(t *testing.T) {
	tests := []struct {
		name   string
		status AlertStatus
	}{
		{name: "silenced", status: AlertStatus{State: "active", SilencedBy: []string{"silence-id"}}},
		{name: "inhibited", status: AlertStatus{State: "active", InhibitedBy: []string{"inhibit-id"}}},
		{name: "muted", status: AlertStatus{State: "active", MutedBy: []string{"mute-id"}}},
		{name: "resolved", status: AlertStatus{State: "resolved"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			notifier := &captureNotifier{}
			processor := NewProcessor(ProcessorOptions{
				LLM:      fakeLLM{},
				Notifier: notifier,
				Deduper:  NewDeduper(time.Hour),
				Logger:   slog.Default(),
			})

			err := processor.processAlerts(context.Background(), "active", "", []Alert{{
				Labels: map[string]string{
					"alertname": "HighCPU",
					"instance":  "node-01",
				},
				Status:   tt.status,
				StartsAt: time.Now(),
			}})
			if err != nil {
				t.Fatalf("processAlerts() error = %v", err)
			}
			if notifier.count != 0 {
				t.Fatalf("notifications = %d, want 0", notifier.count)
			}
		})
	}
}

func TestProcessAlertsDedupesSimilarAlertsIgnoringFingerprint(t *testing.T) {
	notifier := &captureNotifier{}
	processor := NewProcessor(ProcessorOptions{
		LLM:      fakeLLM{},
		Notifier: notifier,
		Deduper:  NewDeduper(time.Hour),
		Logger:   slog.Default(),
	})

	alerts := []Alert{
		{
			Labels: map[string]string{
				"alertname": "HighCPU",
				"severity":  "critical",
				"instance":  "node-01",
				"job":       "node-exporter",
			},
			Fingerprint: "fingerprint-a",
			Status:      AlertStatus{State: "active"},
			StartsAt:    time.Now(),
		},
		{
			Labels: map[string]string{
				"alertname": "HighCPU",
				"severity":  "critical",
				"instance":  "node-01",
				"job":       "node-exporter",
			},
			Fingerprint: "fingerprint-b",
			Status:      AlertStatus{State: "active"},
			StartsAt:    time.Now(),
		},
	}

	if err := processor.processAlerts(context.Background(), "active", "", alerts); err != nil {
		t.Fatalf("processAlerts() error = %v", err)
	}
	if notifier.count != 1 {
		t.Fatalf("notifications = %d, want 1", notifier.count)
	}
}
