package alertmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"xy-sre-agent/internal/llm"
)

type Notifier interface {
	SendText(ctx context.Context, text string) error
}

type ProcessorOptions struct {
	Client    Client
	LLM       llm.Client
	Notifier  Notifier
	Deduper   *Deduper
	Logger    *slog.Logger
	MaxAlerts int
}

type Processor struct {
	client    Client
	llm       llm.Client
	notifier  Notifier
	deduper   *Deduper
	logger    *slog.Logger
	maxAlerts int
}

func NewProcessor(opts ProcessorOptions) *Processor {
	if opts.MaxAlerts <= 0 {
		opts.MaxAlerts = 20
	}
	return &Processor{
		client:    opts.Client,
		llm:       opts.LLM,
		notifier:  opts.Notifier,
		deduper:   opts.Deduper,
		logger:    opts.Logger,
		maxAlerts: opts.MaxAlerts,
	}
}

func (p *Processor) ProcessWebhook(ctx context.Context, payload WebhookPayload) error {
	if len(payload.Alerts) == 0 {
		return nil
	}
	return p.processAlerts(ctx, payload.Status, payload.GroupKey, payload.Alerts)
}

func (p *Processor) ProcessActiveAlerts(ctx context.Context) error {
	alerts, err := p.client.GetActiveAlerts(ctx)
	if err != nil {
		return err
	}
	return p.processAlerts(ctx, "active", "", alerts)
}

func (p *Processor) RunPoller(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		if err := p.ProcessActiveAlerts(ctx); err != nil {
			p.logger.Error("poll active alerts failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (p *Processor) processAlerts(ctx context.Context, status, groupKey string, alerts []Alert) error {
	limit := p.maxAlerts
	if len(alerts) < limit {
		limit = len(alerts)
	}

	for _, alert := range alerts[:limit] {
		if shouldSkipAlert(status, alert) {
			p.logger.Info("skip inactive or suppressed alert",
				"alert", alert.Name(),
				"state", alert.Status.State,
				"silenced_by", len(alert.Status.SilencedBy),
				"inhibited_by", len(alert.Status.InhibitedBy),
				"muted_by", len(alert.Status.MutedBy),
			)
			continue
		}
		dedupKey := alert.DedupKey()
		if groupKey != "" {
			dedupKey = "group=" + groupKey + "|" + dedupKey
		}
		if p.deduper != nil && !p.deduper.ShouldNotify(dedupKey) {
			p.logger.Info("skip duplicated alert", "alert", alert.Name(), "dedup_key", dedupKey, "fingerprint", alert.Fingerprint)
			continue
		}
		analysis, err := p.AnalyzeAlert(ctx, alert)
		if err != nil {
			p.logger.Error("analyze alert failed", "alert", alert.Name(), "error", err)
			analysis = fallbackAlertAnalysis(alert, err)
		}
		if err := p.notifier.SendText(ctx, analysis); err != nil {
			return err
		}
	}
	return nil
}

func shouldSkipAlert(payloadStatus string, alert Alert) bool {
	if strings.EqualFold(payloadStatus, "resolved") || strings.EqualFold(alert.Status.State, "resolved") {
		return true
	}
	if strings.EqualFold(alert.Status.State, "suppressed") || alert.Suppressed() {
		return true
	}
	return !alert.Active()
}

func (p *Processor) AnalyzeAlert(ctx context.Context, alert Alert) (string, error) {
	raw, _ := json.MarshalIndent(alert, "", "  ")
	system := `你是企业级 SRE 告警分析专家。请用中文输出结构化分析，必须包含：告警含义、可能原因、排查步骤、修复建议、风险等级、是否需要人工介入。回答要具体、克制、可执行。`
	user := fmt.Sprintf("请分析以下 Alertmanager active 告警：\n%s", string(raw))
	answer, err := p.llm.ChatCompletion(ctx, llm.ChatCompletionRequest{
		SystemPrompt: system,
		UserPrompt:   user,
		Temperature:  0.2,
	})
	if err != nil {
		return "", err
	}
	return formatAlertMessage(alert, answer), nil
}

func fallbackAlertAnalysis(alert Alert, cause error) string {
	return formatAlertMessage(alert, fmt.Sprintf("LLM 分析失败：%v\n\n基础判断：该告警当前处于 active 状态，请优先检查 labels/annotations 中指向的服务、实例、Pod 或节点，并结合 Prometheus 指标、kubectl describe 与日志确认影响面。", cause))
}

func formatAlertMessage(alert Alert, analysis string) string {
	return fmt.Sprintf("AI-SRE 告警分析\n告警：%s\n级别：%s\n开始时间：%s\n\n%s",
		alert.Name(),
		alert.Severity(),
		alert.StartsAt.Format(time.RFC3339),
		strings.TrimSpace(analysis),
	)
}
