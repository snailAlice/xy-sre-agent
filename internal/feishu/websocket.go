package feishu

import (
	"context"
	"fmt"
	"log/slog"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"

	"xy-sre-agent/internal/config"
)

type WebSocketReceiver struct {
	cfg    config.FeishuConfig
	client *Client
	agent  Agent
	logger *slog.Logger
}

func NewWebSocketReceiver(cfg config.FeishuConfig, client *Client, agent Agent, logger *slog.Logger) *WebSocketReceiver {
	return &WebSocketReceiver{cfg: cfg, client: client, agent: agent, logger: logger}
}

func (r *WebSocketReceiver) Run(ctx context.Context) error {
	if r.cfg.AppID == "" || r.cfg.AppSecret == "" {
		return fmt.Errorf("feishu.app_id/app_secret is required for websocket event mode")
	}

	eventHandler := dispatcher.NewEventDispatcher("", "").
		OnP2MessageReceiveV1(func(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
			return r.handleMessage(ctx, event)
		})

	wsClient := larkws.NewClient(
		r.cfg.AppID,
		r.cfg.AppSecret,
		larkws.WithEventHandler(eventHandler),
		larkws.WithLogLevel(larkcore.LogLevelInfo),
	)

	r.logger.Info("starting feishu websocket event receiver")
	return wsClient.Start(ctx)
}

func (r *WebSocketReceiver) handleMessage(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
	if event == nil || event.Event == nil || event.Event.Message == nil {
		return nil
	}

	message := event.Event.Message
	messageID := stringValue(message.MessageId)
	messageType := stringValue(message.MessageType)
	chatID := stringValue(message.ChatId)
	text := stripAtText(parseTextContent(stringValue(message.Content)))

	if messageID == "" {
		r.logger.Warn("feishu websocket message missing message_id")
		return nil
	}
	if !r.client.MarkMessageProcessing(messageID) {
		r.logger.Info("skip duplicated feishu websocket message", "message_id", messageID, "chat_id", chatID)
		return nil
	}
	if messageType != "" && messageType != "text" {
		return r.client.ReplyText(ctx, messageID, "当前只支持文本消息，请直接用文字描述你要排查的问题。")
	}

	sessionID := firstNonEmpty(chatID, senderOpenID(event), "feishu-websocket")
	r.logger.Info("received feishu websocket message", "message_id", messageID, "chat_id", chatID, "message_type", messageType)

	answer, err := r.agent.HandleMessage(ctx, sessionID, text)
	if err != nil {
		answer = fmt.Sprintf("AI-SRE 处理失败：%v", err)
	}
	if err := r.client.ReplyText(ctx, messageID, answer); err != nil {
		r.logger.Error("reply feishu websocket message failed", "message_id", messageID, "error", err)
		return err
	}
	return nil
}

func senderOpenID(event *larkim.P2MessageReceiveV1) string {
	if event == nil || event.Event == nil || event.Event.Sender == nil || event.Event.Sender.SenderId == nil {
		return ""
	}
	return stringValue(event.Event.Sender.SenderId.OpenId)
}

func stringValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
