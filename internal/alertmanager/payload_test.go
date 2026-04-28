package alertmanager

import "testing"

func TestParseWebhookPayloadAcceptsStandardWebhookStatusString(t *testing.T) {
	raw := []byte(`{
		"receiver": "ai-sre-agent",
		"status": "firing",
		"alerts": [
			{
				"status": "firing",
				"labels": {
					"alertname": "TestHighCPU",
					"severity": "critical"
				},
				"annotations": {
					"summary": "CPU usage too high"
				},
				"startsAt": "2026-04-28T10:00:00.000Z",
				"endsAt": "2026-04-28T11:00:00.000Z",
				"generatorURL": "http://prometheus.example.com/graph?g0.expr=cpu",
				"fingerprint": "fee020d18df58fb8"
			}
		]
	}`)

	payload, err := ParseWebhookPayload(raw)
	if err != nil {
		t.Fatalf("ParseWebhookPayload() error = %v", err)
	}
	if len(payload.Alerts) != 1 {
		t.Fatalf("alerts len = %d, want 1", len(payload.Alerts))
	}
	if payload.Alerts[0].Status.State != "active" {
		t.Fatalf("alert state = %q, want active", payload.Alerts[0].Status.State)
	}
}

func TestParseWebhookPayloadAcceptsAlertAPIArray(t *testing.T) {
	raw := []byte(`[
		{
			"status": {
				"state": "active",
				"silencedBy": [],
				"inhibitedBy": [],
				"mutedBy": []
			},
			"labels": {
				"alertname": "HighMemoryUsage",
				"severity": "critical"
			},
			"annotations": {
				"summary": "节点内存使用率过高"
			},
			"startsAt": "2026-04-28T01:00:00Z",
			"endsAt": "2026-04-29T01:00:00Z",
			"generatorURL": "http://prometheus:9090/graph?g0.expr=memory"
		}
	]`)

	payload, err := ParseWebhookPayload(raw)
	if err != nil {
		t.Fatalf("ParseWebhookPayload() error = %v", err)
	}
	if payload.Status != "active" {
		t.Fatalf("payload status = %q, want active", payload.Status)
	}
	if got := payload.Alerts[0].Name(); got != "HighMemoryUsage" {
		t.Fatalf("alert name = %q, want HighMemoryUsage", got)
	}
}
