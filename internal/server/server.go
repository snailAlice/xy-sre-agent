package server

import (
	"context"
	"log/slog"
	"net/http"

	"xy-sre-agent/internal/alertmanager"
	"xy-sre-agent/internal/config"
)

type Agent interface {
	HandleMessage(ctx context.Context, sessionID, userText string) (string, error)
}

type AlertProcessor interface {
	ProcessWebhook(ctx context.Context, payload alertmanager.WebhookPayload) error
}

type Options struct {
	Config         *config.Config
	Logger         *slog.Logger
	Agent          Agent
	AlertProcessor AlertProcessor
	FeishuHandler  http.Handler
	AlertClient    alertmanager.Client
}

type Server struct {
	cfg            *config.Config
	logger         *slog.Logger
	agent          Agent
	alertProcessor AlertProcessor
	feishuHandler  http.Handler
	alertClient    alertmanager.Client
}

func New(opts Options) *Server {
	return &Server{
		cfg:            opts.Config,
		logger:         opts.Logger,
		agent:          opts.Agent,
		alertProcessor: opts.AlertProcessor,
		feishuHandler:  opts.FeishuHandler,
		alertClient:    opts.AlertClient,
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("POST /api/alert/webhook", s.alertWebhook)
	mux.HandleFunc("GET /api/alert/active", s.activeAlerts)
	mux.HandleFunc("POST /api/chat", s.chat)
	mux.Handle("/api/feishu/events", s.feishuHandler)
	return requestLogger(s.logger, mux)
}
