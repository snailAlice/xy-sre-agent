package alertmanager

import (
	"encoding/json"
	"fmt"
	"strings"
)

func ParseWebhookPayload(data []byte) (WebhookPayload, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return WebhookPayload{}, fmt.Errorf("empty payload")
	}

	if strings.HasPrefix(trimmed, "[") {
		var alerts []Alert
		if err := json.Unmarshal(data, &alerts); err != nil {
			return WebhookPayload{}, err
		}
		return WebhookPayload{
			Status: "active",
			Alerts: alerts,
		}, nil
	}

	var payload WebhookPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return WebhookPayload{}, err
	}
	return payload, nil
}
