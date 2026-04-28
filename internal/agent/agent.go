package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"xy-sre-agent/internal/llm"
	"xy-sre-agent/internal/memory"
	"xy-sre-agent/internal/tools"
)

type Options struct {
	LLM              llm.Client
	Tools            *tools.Manager
	Memory           memory.Store
	Logger           *slog.Logger
	DefaultNamespace string
}

type Agent struct {
	llm              llm.Client
	tools            *tools.Manager
	memory           memory.Store
	logger           *slog.Logger
	defaultNamespace string
}

type ToolRun struct {
	Name string
	Args map[string]any
}

func New(opts Options) *Agent {
	return &Agent{
		llm:              opts.LLM,
		tools:            opts.Tools,
		memory:           opts.Memory,
		logger:           opts.Logger,
		defaultNamespace: opts.DefaultNamespace,
	}
}

func (a *Agent) HandleMessage(ctx context.Context, sessionID, userText string) (string, error) {
	userText = strings.TrimSpace(userText)
	if userText == "" {
		return "请告诉我你想查询的告警、服务、Pod 或指标。", nil
	}

	_ = a.memory.Append(ctx, sessionID, memory.Message{Role: "user", Content: userText})
	history, _ := a.memory.List(ctx, sessionID)

	plan := a.plan(userText)
	a.logger.Info("agent tool plan", "question", userText, "tools", toolNames(plan))
	results := make([]tools.Result, 0, len(plan))
	for _, run := range plan {
		result, err := a.tools.Execute(ctx, run.Name, run.Args)
		if err != nil {
			result = tools.Result{
				Name:      run.Name,
				Summary:   "工具执行失败",
				Output:    err.Error(),
				StartedAt: time.Now(),
				Duration:  "0s",
			}
			a.logger.Warn("tool failed", "tool", run.Name, "error", err)
		}
		results = append(results, result)
	}
	results = append(results, a.runFollowUpTools(ctx, userText, results)...)

	answer, err := a.summarize(ctx, userText, history, results)
	if err != nil {
		return fallbackAnswer(results, err), nil
	}
	_ = a.memory.Append(ctx, sessionID, memory.Message{Role: "assistant", Content: answer})
	return answer, nil
}

func (a *Agent) runFollowUpTools(ctx context.Context, text string, results []tools.Result) []tools.Result {
	lower := strings.ToLower(text)
	service := extractServiceName(text)
	if extractPodName(text) != "" || service == "" {
		return nil
	}
	ns := extractNamespace(text, a.defaultNamespace)
	var pod string
	for _, result := range results {
		if result.Name == "get_active_pods" {
			pod = findPodByService(result.Output, service)
			break
		}
	}
	if pod == "" {
		return nil
	}

	var runs []ToolRun
	if containsAny(lower, "pod", "重启", "restart", "crashloop", "crash", "为什么", "原因", "分析") {
		runs = append(runs, ToolRun{Name: "describe_pod", Args: map[string]any{"namespace": ns, "pod": pod}})
	}
	if containsAny(lower, "日志", "log", "报错", "error", "exception") {
		runs = append(runs, ToolRun{Name: "get_pod_logs", Args: map[string]any{"namespace": ns, "pod": pod}})
	}

	out := make([]tools.Result, 0, len(runs))
	for _, run := range dedupeRuns(runs) {
		result, err := a.tools.Execute(ctx, run.Name, run.Args)
		if err != nil {
			result = tools.Result{
				Name:      run.Name,
				Summary:   "工具执行失败",
				Output:    err.Error(),
				StartedAt: time.Now(),
				Duration:  "0s",
			}
			a.logger.Warn("follow-up tool failed", "tool", run.Name, "error", err)
		}
		out = append(out, result)
	}
	return out
}

func (a *Agent) plan(text string) []ToolRun {
	lower := strings.ToLower(text)
	ns := extractNamespace(text, a.defaultNamespace)
	pod := extractPodName(text)
	service := extractServiceName(text)
	var runs []ToolRun

	if containsAny(lower, "当前", "active", "告警", "报警", "alert", "alerts", "最近告警") {
		runs = append(runs, ToolRun{Name: "get_active_alerts", Args: map[string]any{}})
	}
	if containsAny(lower, "node", "节点") {
		runs = append(runs, ToolRun{Name: "get_nodes", Args: map[string]any{}})
	}
	if containsAny(lower, "pod", "容器", "重启", "restart", "crashloop", "crash", "异常 pod") {
		runs = append(runs, ToolRun{Name: "get_active_pods", Args: map[string]any{"namespace": ns}})
		if pod != "" {
			runs = append(runs, ToolRun{Name: "describe_pod", Args: map[string]any{"namespace": ns, "pod": pod}})
		}
	}
	if containsAny(lower, "日志", "log", "报错", "error", "exception") && pod != "" {
		runs = append(runs, ToolRun{Name: "get_pod_logs", Args: map[string]any{"namespace": ns, "pod": pod}})
	}
	if containsAny(lower, "cpu", "内存", "memory", "qps", "错误率", "error rate", "磁盘", "disk") {
		runs = append(runs, ToolRun{Name: "prom_query", Args: map[string]any{"promql": buildPromQL(lower, service)}})
	}
	if containsAny(lower, "为什么", "原因", "分析") && len(runs) == 0 {
		runs = append(runs, ToolRun{Name: "get_active_alerts", Args: map[string]any{}})
		if service != "" {
			runs = append(runs, ToolRun{Name: "get_active_pods", Args: map[string]any{"namespace": ns}})
		}
	}
	return dedupeRuns(runs)
}

func (a *Agent) summarize(ctx context.Context, question string, history []memory.Message, results []tools.Result) (string, error) {
	rawResults, _ := json.MarshalIndent(results, "", "  ")
	var hist strings.Builder
	for _, msg := range history {
		hist.WriteString(msg.Role)
		hist.WriteString(": ")
		hist.WriteString(msg.Content)
		hist.WriteString("\n")
	}

	system := `你是企业级 AI-SRE Agent。你会基于工具结果回答用户，不编造不存在的集群事实。输出中文，优先给结论，然后列证据、可能原因、下一步排查或修复动作。工具失败时要明确说明失败原因和替代建议。`
	user := fmt.Sprintf("用户问题：%s\n\n最近对话：\n%s\n\n工具结果 JSON：\n%s", question, hist.String(), string(rawResults))
	return a.llm.ChatCompletion(ctx, llm.ChatCompletionRequest{
		SystemPrompt: system,
		UserPrompt:   user,
		Temperature:  0.2,
	})
}

func fallbackAnswer(results []tools.Result, cause error) string {
	raw, _ := json.MarshalIndent(results, "", "  ")
	return fmt.Sprintf("LLM 暂时不可用：%v\n\n已执行工具结果如下，可先基于这些信息人工判断：\n%s", cause, string(raw))
}

func containsAny(text string, keys ...string) bool {
	for _, key := range keys {
		if strings.Contains(text, strings.ToLower(key)) {
			return true
		}
	}
	return false
}

var namespacePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(namespace|ns|-n)\s*[:=]?\s*([A-Za-z0-9_.-]+)`),
	regexp.MustCompile(`命名空间\s*([A-Za-z0-9_.-]+)`),
}

func extractNamespace(text, fallback string) string {
	for _, re := range namespacePatterns {
		if m := re.FindStringSubmatch(text); len(m) >= 3 {
			return m[2]
		}
		if m := re.FindStringSubmatch(text); len(m) == 2 {
			return m[1]
		}
	}
	if strings.Contains(text, "prod") {
		return "prod"
	}
	return fallback
}

var podPattern = regexp.MustCompile(`([A-Za-z0-9][A-Za-z0-9_.-]*-[A-Za-z0-9][A-Za-z0-9_.-]*-[A-Za-z0-9][A-Za-z0-9_.-]*)`)

func extractPodName(text string) string {
	if m := podPattern.FindStringSubmatch(text); len(m) == 2 {
		return m[1]
	}
	return ""
}

func extractServiceName(text string) string {
	for _, token := range strings.Fields(text) {
		token = strings.Trim(token, "，。,.?？:：")
		if token == "" {
			continue
		}
		lower := strings.ToLower(token)
		if lower == "mysql" || lower == "redis" {
			return token
		}
		if lower == "pod" || lower == "cpu" || lower == "log" || lower == "logs" || lower == "node" {
			continue
		}
		if regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{2,}$`).MatchString(token) {
			return token
		}
	}
	if strings.Contains(strings.ToLower(text), "mysql") {
		return "mysql"
	}
	if strings.Contains(strings.ToLower(text), "redis") {
		return "redis"
	}
	if strings.Contains(strings.ToLower(text), "payment") {
		return "payment"
	}
	return ""
}

func buildPromQL(text, service string) string {
	filter := ""
	if service != "" {
		filter = fmt.Sprintf(`,pod=~".*%s.*"`, service)
	}
	switch {
	case strings.Contains(text, "内存") || strings.Contains(text, "memory"):
		return fmt.Sprintf(`sum(container_memory_working_set_bytes{container!="",container!="POD"%s}) by (pod)`, filter)
	case strings.Contains(text, "qps"):
		return fmt.Sprintf(`sum(rate(http_requests_total{%s}[5m])) by (service)`, strings.TrimPrefix(filter, ","))
	case strings.Contains(text, "错误率") || strings.Contains(text, "error"):
		return fmt.Sprintf(`sum(rate(http_requests_total{status=~"5.."%s}[5m])) by (service) / sum(rate(http_requests_total{%s}[5m])) by (service)`, filter, strings.TrimPrefix(filter, ","))
	case strings.Contains(text, "磁盘") || strings.Contains(text, "disk"):
		return `100 - (node_filesystem_avail_bytes{fstype!~"tmpfs|overlay"} * 100 / node_filesystem_size_bytes{fstype!~"tmpfs|overlay"})`
	default:
		return fmt.Sprintf(`sum(rate(container_cpu_usage_seconds_total{container!="",container!="POD"%s}[5m])) by (pod)`, filter)
	}
}

func dedupeRuns(in []ToolRun) []ToolRun {
	seen := make(map[string]struct{})
	out := make([]ToolRun, 0, len(in))
	for _, run := range in {
		raw, _ := json.Marshal(run.Args)
		key := run.Name + ":" + string(raw)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, run)
	}
	return out
}

func toolNames(runs []ToolRun) []string {
	names := make([]string, 0, len(runs))
	for _, run := range runs {
		names = append(names, run.Name)
	}
	return names
}

func findPodByService(kubectlOutput, service string) string {
	service = strings.ToLower(service)
	lines := strings.Split(kubectlOutput, "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.EqualFold(fields[0], "NAME") {
			continue
		}
		name := fields[0]
		if strings.Contains(strings.ToLower(name), service) {
			return name
		}
	}
	return ""
}
