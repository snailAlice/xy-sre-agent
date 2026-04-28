package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"xy-sre-agent/internal/agent"
	"xy-sre-agent/internal/alertmanager"
	"xy-sre-agent/internal/config"
	"xy-sre-agent/internal/feishu"
	"xy-sre-agent/internal/llm"
	"xy-sre-agent/internal/memory"
	"xy-sre-agent/internal/server"
	"xy-sre-agent/internal/tools"
	"xy-sre-agent/pkg/logx"
)

func main() {
	configPath := flag.String("config", getenv("CONFIG_PATH", "config.yaml"), "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	logger := logx.New(cfg.Log.Level)
	slog.SetDefault(logger)

	httpClient := &http.Client{Timeout: cfg.HTTP.Timeout}
	alertClient := alertmanager.NewClient(cfg.Alertmanager, httpClient, logger)
	llmClient := llm.NewClient(cfg.DeepSeek, httpClient, logger)
	feishuClient := feishu.NewClient(cfg.Feishu, httpClient, logger)
	memStore := memory.NewInMemoryStore(cfg.Agent.MemoryTTL, cfg.Agent.MaxHistory)

	toolManager := tools.NewManager(logger)
	toolManager.Register(tools.NewGetActiveAlertsTool(alertClient))
	toolManager.Register(tools.NewGetPodsTool(cfg.Kubernetes.Kubeconfig, cfg.Kubernetes.DefaultNamespace, logger))
	toolManager.Register(tools.NewDescribePodTool(cfg.Kubernetes.Kubeconfig, cfg.Kubernetes.DefaultNamespace, logger))
	toolManager.Register(tools.NewGetPodLogsTool(cfg.Kubernetes.Kubeconfig, cfg.Kubernetes.DefaultNamespace, cfg.Kubernetes.LogTailLines, logger))
	toolManager.Register(tools.NewGetNodesTool(cfg.Kubernetes.Kubeconfig, logger))
	toolManager.Register(tools.NewPromQueryTool(cfg.Prometheus, httpClient, logger))

	sreAgent := agent.New(agent.Options{
		LLM:              llmClient,
		Tools:            toolManager,
		Memory:           memStore,
		Logger:           logger,
		DefaultNamespace: cfg.Kubernetes.DefaultNamespace,
	})

	alertProcessor := alertmanager.NewProcessor(alertmanager.ProcessorOptions{
		Client:    alertClient,
		LLM:       llmClient,
		Notifier:  feishuClient,
		Deduper:   alertmanager.NewDeduper(cfg.Alertmanager.DedupeTTL),
		Logger:    logger,
		MaxAlerts: cfg.Alertmanager.MaxAlertsPerWebhook,
	})
	rootCtx, cancelRoot := context.WithCancel(context.Background())
	defer cancelRoot()
	if cfg.Alertmanager.PollInterval > 0 {
		go alertProcessor.RunPoller(rootCtx, cfg.Alertmanager.PollInterval)
	}
	if cfg.Feishu.EventMode == "websocket" {
		wsReceiver := feishu.NewWebSocketReceiver(cfg.Feishu, feishuClient, sreAgent, logger)
		go func() {
			if err := wsReceiver.Run(rootCtx); err != nil && rootCtx.Err() == nil {
				logger.Error("feishu websocket receiver stopped", "error", err)
			}
		}()
	}

	app := server.New(server.Options{
		Config:         cfg,
		Logger:         logger,
		Agent:          sreAgent,
		AlertProcessor: alertProcessor,
		FeishuHandler:  feishu.NewEventHandler(cfg.Feishu, feishuClient, sreAgent, logger),
		AlertClient:    alertClient,
	})

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:           app.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info("ai-sre-agent started", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server stopped unexpectedly", "error", err)
			os.Exit(1)
		}
	}()

	waitForShutdown(logger, srv, cancelRoot)
}

func waitForShutdown(logger *slog.Logger, srv *http.Server, cancelPoller context.CancelFunc) {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	cancelPoller()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	logger.Info("shutting down server")
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
