package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"regexp"
	"strconv"
	"time"

	"xy-sre-agent/internal/llm"
)

var k8sNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

type kubectlRunner struct {
	kubeconfig string
	logger     *slog.Logger
}

func (r kubectlRunner) run(ctx context.Context, args ...string) (string, error) {
	finalArgs := make([]string, 0, len(args)+2)
	if r.kubeconfig != "" {
		finalArgs = append(finalArgs, "--kubeconfig", r.kubeconfig)
	}
	finalArgs = append(finalArgs, args...)

	cmd := exec.CommandContext(ctx, "kubectl", finalArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("kubectl %v failed: %w: %s", args, err, stderr.String())
	}
	return stdout.String(), nil
}

func cleanNamespace(namespace, fallback string) (string, error) {
	if namespace == "" {
		namespace = fallback
	}
	if !k8sNamePattern.MatchString(namespace) {
		return "", fmt.Errorf("invalid namespace %q", namespace)
	}
	return namespace, nil
}

func cleanK8sName(kind, value string) (string, error) {
	if value == "" || !k8sNamePattern.MatchString(value) {
		return "", fmt.Errorf("invalid %s %q", kind, value)
	}
	return value, nil
}

type GetPodsTool struct {
	runner           kubectlRunner
	defaultNamespace string
}

func NewGetPodsTool(kubeconfig, defaultNamespace string, logger *slog.Logger) *GetPodsTool {
	return &GetPodsTool{runner: kubectlRunner{kubeconfig: kubeconfig, logger: logger}, defaultNamespace: defaultNamespace}
}

func (t *GetPodsTool) Name() string { return "get_active_pods" }
func (t *GetPodsTool) Description() string {
	return "查询指定 namespace 下的 Pod 状态，等价于 kubectl get pods -n <namespace> -o wide"
}
func (t *GetPodsTool) Definition() llm.ToolSpec {
	return llm.ToolSpec{Type: "function", Function: llm.ToolFunctionSpec{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: schema(map[string]any{
			"namespace": map[string]any{"type": "string", "description": "Kubernetes namespace，缺省使用默认命名空间"},
		}, nil),
	}}
}
func (t *GetPodsTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	start := time.Now()
	var input struct {
		Namespace string `json:"namespace"`
	}
	_ = json.Unmarshal(raw, &input)
	ns, err := cleanNamespace(input.Namespace, t.defaultNamespace)
	if err != nil {
		return Result{}, err
	}
	out, err := t.runner.run(ctx, "get", "pods", "-n", ns, "-o", "wide")
	return Result{Name: t.Name(), Summary: "Pod 状态列表", Output: out, StartedAt: start, Duration: time.Since(start).String(), Metadata: map[string]any{"namespace": ns}}, err
}

type DescribePodTool struct {
	runner           kubectlRunner
	defaultNamespace string
}

func NewDescribePodTool(kubeconfig, defaultNamespace string, logger *slog.Logger) *DescribePodTool {
	return &DescribePodTool{runner: kubectlRunner{kubeconfig: kubeconfig, logger: logger}, defaultNamespace: defaultNamespace}
}

func (t *DescribePodTool) Name() string { return "describe_pod" }
func (t *DescribePodTool) Description() string {
	return "查看 Pod 详情和事件，等价于 kubectl describe pod <pod> -n <namespace>"
}
func (t *DescribePodTool) Definition() llm.ToolSpec {
	return llm.ToolSpec{Type: "function", Function: llm.ToolFunctionSpec{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: schema(map[string]any{
			"namespace": map[string]any{"type": "string"},
			"pod":       map[string]any{"type": "string"},
		}, []string{"pod"}),
	}}
}
func (t *DescribePodTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	start := time.Now()
	var input struct {
		Namespace string `json:"namespace"`
		Pod       string `json:"pod"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{}, err
	}
	ns, err := cleanNamespace(input.Namespace, t.defaultNamespace)
	if err != nil {
		return Result{}, err
	}
	pod, err := cleanK8sName("pod", input.Pod)
	if err != nil {
		return Result{}, err
	}
	out, err := t.runner.run(ctx, "describe", "pod", pod, "-n", ns)
	return Result{Name: t.Name(), Summary: "Pod describe 结果", Output: out, StartedAt: start, Duration: time.Since(start).String(), Metadata: map[string]any{"namespace": ns, "pod": pod}}, err
}

type GetPodLogsTool struct {
	runner           kubectlRunner
	defaultNamespace string
	tailLines        int
}

func NewGetPodLogsTool(kubeconfig, defaultNamespace string, tailLines int, logger *slog.Logger) *GetPodLogsTool {
	return &GetPodLogsTool{runner: kubectlRunner{kubeconfig: kubeconfig, logger: logger}, defaultNamespace: defaultNamespace, tailLines: tailLines}
}

func (t *GetPodLogsTool) Name() string { return "get_pod_logs" }
func (t *GetPodLogsTool) Description() string {
	return "查看 Pod 最近日志，等价于 kubectl logs <pod> -n <namespace> --tail=<lines>"
}
func (t *GetPodLogsTool) Definition() llm.ToolSpec {
	return llm.ToolSpec{Type: "function", Function: llm.ToolFunctionSpec{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: schema(map[string]any{
			"namespace": map[string]any{"type": "string"},
			"pod":       map[string]any{"type": "string"},
			"tail":      map[string]any{"type": "integer", "description": "日志行数，默认使用配置值"},
		}, []string{"pod"}),
	}}
}
func (t *GetPodLogsTool) Execute(ctx context.Context, raw json.RawMessage) (Result, error) {
	start := time.Now()
	var input struct {
		Namespace string `json:"namespace"`
		Pod       string `json:"pod"`
		Tail      int    `json:"tail"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return Result{}, err
	}
	ns, err := cleanNamespace(input.Namespace, t.defaultNamespace)
	if err != nil {
		return Result{}, err
	}
	pod, err := cleanK8sName("pod", input.Pod)
	if err != nil {
		return Result{}, err
	}
	tail := input.Tail
	if tail <= 0 {
		tail = t.tailLines
	}
	out, err := t.runner.run(ctx, "logs", pod, "-n", ns, "--tail="+strconv.Itoa(tail))
	return Result{Name: t.Name(), Summary: "Pod 最近日志", Output: out, StartedAt: start, Duration: time.Since(start).String(), Metadata: map[string]any{"namespace": ns, "pod": pod, "tail": tail}}, err
}

type GetNodesTool struct {
	runner kubectlRunner
}

func NewGetNodesTool(kubeconfig string, logger *slog.Logger) *GetNodesTool {
	return &GetNodesTool{runner: kubectlRunner{kubeconfig: kubeconfig, logger: logger}}
}

func (t *GetNodesTool) Name() string { return "get_nodes" }
func (t *GetNodesTool) Description() string {
	return "查询 Kubernetes 节点状态，等价于 kubectl get nodes -o wide"
}
func (t *GetNodesTool) Definition() llm.ToolSpec {
	return llm.ToolSpec{Type: "function", Function: llm.ToolFunctionSpec{Name: t.Name(), Description: t.Description(), Parameters: schema(map[string]any{}, nil)}}
}
func (t *GetNodesTool) Execute(ctx context.Context, _ json.RawMessage) (Result, error) {
	start := time.Now()
	out, err := t.runner.run(ctx, "get", "nodes", "-o", "wide")
	return Result{Name: t.Name(), Summary: "节点状态列表", Output: out, StartedAt: start, Duration: time.Since(start).String()}, err
}
