package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"xy-sre-agent/internal/config"
)

type Agent interface {
	HandleMessage(ctx context.Context, sessionID, userText string) (string, error)
}

type EventHandler struct {
	cfg    config.FeishuConfig
	client *Client
	agent  Agent
	logger *slog.Logger
}

type eventEnvelope struct {
	Schema    string          `json:"schema"`
	Challenge string          `json:"challenge"`
	Token     string          `json:"token"`
	Type      string          `json:"type"`
	Header    eventHeader     `json:"header"`
	Event     json.RawMessage `json:"event"`
}

type eventHeader struct {
	EventID   string `json:"event_id"`
	EventType string `json:"event_type"`
	Token     string `json:"token"`
}

type messageReceiveEvent struct {
	Sender struct {
		SenderID struct {
			OpenID string `json:"open_id"`
			UserID string `json:"user_id"`
		} `json:"sender_id"`
	} `json:"sender"`
	Message struct {
		MessageID   string `json:"message_id"`
		ChatID      string `json:"chat_id"`
		MessageType string `json:"message_type"`
		Content     string `json:"content"`
	} `json:"message"`
}

func NewEventHandler(cfg config.FeishuConfig, client *Client, agent Agent, logger *slog.Logger) *EventHandler {
	return &EventHandler{cfg: cfg, client: client, agent: agent, logger: logger}
}

func (h *EventHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		http.Error(w, "read body failed", http.StatusBadRequest)
		return
	}
	if !verifySignature(r, body, h.cfg.EventSecret) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	var env eventEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if env.Challenge != "" {
		if !verifyToken(h.cfg.VerificationToken, env.Token) {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		writeJSON(w, map[string]string{"challenge": env.Challenge})
		return
	}
	if !verifyToken(h.cfg.VerificationToken, firstNonEmpty(env.Header.Token, env.Token)) {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}
	if env.Header.EventType != "im.message.receive_v1" {
		writeJSON(w, map[string]string{"status": "ignored"})
		return
	}

	var event messageReceiveEvent
	if err := json.Unmarshal(env.Event, &event); err != nil {
		http.Error(w, "invalid event", http.StatusBadRequest)
		return
	}
	text := parseTextContent(event.Message.Content)
	sessionID := firstNonEmpty(event.Message.ChatID, event.Sender.SenderID.OpenID, "feishu")

	if !h.client.MarkMessageProcessing(event.Message.MessageID) {
		h.logger.Info("skip duplicated feishu message", "message_id", event.Message.MessageID, "chat_id", event.Message.ChatID)
		writeJSON(w, map[string]string{"status": "duplicated"})
		return
	}

	go h.replyAsync(context.Background(), sessionID, event.Message.MessageID, text)
	writeJSON(w, map[string]string{"status": "ok"})
}

func (h *EventHandler) replyAsync(ctx context.Context, sessionID, messageID, text string) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	answer, err := h.agent.HandleMessage(ctx, sessionID, stripAtText(text))
	if err != nil {
		answer = fmt.Sprintf("AI-SRE 处理失败：%v", err)
	}
	if err := h.client.ReplyText(ctx, messageID, answer); err != nil {
		h.logger.Error("reply feishu message failed", "error", err)
	}
}

func parseTextContent(content string) string {
	var payload struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(content), &payload); err == nil && payload.Text != "" {
		return payload.Text
	}
	return content
}

func stripAtText(text string) string {
	fields := strings.Fields(text)
	kept := fields[:0]
	for _, field := range fields {
		if strings.HasPrefix(field, "@") {
			continue
		}
		kept = append(kept, field)
	}
	return strings.TrimSpace(strings.Join(kept, " "))
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(payload)
}
