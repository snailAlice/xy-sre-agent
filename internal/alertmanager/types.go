package alertmanager

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type Alert struct {
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       time.Time         `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL"`
	Fingerprint  string            `json:"fingerprint"`
	Status       AlertStatus       `json:"status"`
	Receivers    []Receiver        `json:"receivers,omitempty"`
	UpdatedAt    time.Time         `json:"updatedAt,omitempty"`
}

type AlertStatus struct {
	State       string   `json:"state"`
	SilencedBy  []string `json:"silencedBy,omitempty"`
	InhibitedBy []string `json:"inhibitedBy,omitempty"`
	MutedBy     []string `json:"mutedBy,omitempty"`
}

type Receiver struct {
	Name string `json:"name"`
}

type WebhookPayload struct {
	Version           string            `json:"version"`
	GroupKey          string            `json:"groupKey"`
	Status            string            `json:"status"`
	Receiver          string            `json:"receiver"`
	GroupLabels       map[string]string `json:"groupLabels"`
	CommonLabels      map[string]string `json:"commonLabels"`
	CommonAnnotations map[string]string `json:"commonAnnotations"`
	ExternalURL       string            `json:"externalURL"`
	Alerts            []Alert           `json:"alerts"`
}

func (s *AlertStatus) UnmarshalJSON(data []byte) error {
	var state string
	if err := json.Unmarshal(data, &state); err == nil {
		s.State = normalizeState(state)
		return nil
	}

	var object struct {
		State       string   `json:"state"`
		SilencedBy  []string `json:"silencedBy"`
		InhibitedBy []string `json:"inhibitedBy"`
		MutedBy     []string `json:"mutedBy"`
	}
	if err := json.Unmarshal(data, &object); err != nil {
		return fmt.Errorf("alert status must be string or object: %w", err)
	}
	s.State = normalizeState(object.State)
	s.SilencedBy = object.SilencedBy
	s.InhibitedBy = object.InhibitedBy
	s.MutedBy = object.MutedBy
	return nil
}

func normalizeState(state string) string {
	switch strings.ToLower(state) {
	case "firing":
		return "active"
	case "resolved":
		return "resolved"
	default:
		return state
	}
}

func (a Alert) Name() string {
	if v := a.Labels["alertname"]; v != "" {
		return v
	}
	return "unknown-alert"
}

func (a Alert) Severity() string {
	if v := a.Labels["severity"]; v != "" {
		return v
	}
	return "unknown"
}

func (a Alert) DedupKey() string {
	key := "alertname=" + a.Name()
	for _, name := range []string{"severity", "namespace", "service", "job", "instance", "pod", "node"} {
		if v := a.Labels[name]; v != "" {
			key += "|" + name + "=" + v
		}
	}
	return key
}

func (a Alert) Suppressed() bool {
	return len(a.Status.SilencedBy) > 0 || len(a.Status.InhibitedBy) > 0 || len(a.Status.MutedBy) > 0
}

func (a Alert) Active() bool {
	state := strings.ToLower(a.Status.State)
	return state == "" || state == "active" || state == "firing"
}
