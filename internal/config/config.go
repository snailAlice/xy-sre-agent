package config

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Server       ServerConfig       `yaml:"server"`
	Log          LogConfig          `yaml:"log"`
	HTTP         HTTPConfig         `yaml:"http"`
	Alertmanager AlertmanagerConfig `yaml:"alertmanager"`
	Prometheus   PrometheusConfig   `yaml:"prometheus"`
	DeepSeek     DeepSeekConfig     `yaml:"deepseek"`
	Feishu       FeishuConfig       `yaml:"feishu"`
	Kubernetes   KubernetesConfig   `yaml:"kubernetes"`
	Agent        AgentConfig        `yaml:"agent"`
}

type ServerConfig struct {
	Port int `yaml:"port"`
}

type LogConfig struct {
	Level string `yaml:"level"`
}

type HTTPConfig struct {
	Timeout time.Duration `yaml:"timeout"`
}

type AlertmanagerConfig struct {
	URL                 string        `yaml:"url"`
	Token               string        `yaml:"token"`
	PollInterval        time.Duration `yaml:"poll_interval"`
	DedupeTTL           time.Duration `yaml:"dedupe_ttl"`
	MaxAlertsPerWebhook int           `yaml:"max_alerts_per_webhook"`
}

type PrometheusConfig struct {
	URL   string `yaml:"url"`
	Token string `yaml:"token"`
}

type DeepSeekConfig struct {
	APIKey     string        `yaml:"api_key"`
	BaseURL    string        `yaml:"base_url"`
	Model      string        `yaml:"model"`
	Timeout    time.Duration `yaml:"timeout"`
	MaxRetries int           `yaml:"max_retries"`
}

type FeishuConfig struct {
	EventMode         string `yaml:"event_mode"`
	AppID             string `yaml:"app_id"`
	AppSecret         string `yaml:"app_secret"`
	BotToken          string `yaml:"bot_token"`
	VerificationToken string `yaml:"verification_token"`
	EventSecret       string `yaml:"event_secret"`
	APIBaseURL        string `yaml:"api_base_url"`
}

type KubernetesConfig struct {
	Kubeconfig       string `yaml:"kubeconfig"`
	DefaultNamespace string `yaml:"default_namespace"`
	LogTailLines     int    `yaml:"log_tail_lines"`
}

type AgentConfig struct {
	MemoryTTL  time.Duration `yaml:"memory_ttl"`
	MaxHistory int           `yaml:"max_history"`
}

func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	expanded := expandEnv(string(raw))
	cfg := defaultConfig()
	if err := decodeConfigYAML(expanded, &cfg); err != nil {
		return nil, err
	}
	cfg.applyEnvOverrides()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func defaultConfig() Config {
	return Config{
		Server: ServerConfig{Port: 8080},
		Log:    LogConfig{Level: "info"},
		HTTP:   HTTPConfig{Timeout: 30 * time.Second},
		Alertmanager: AlertmanagerConfig{
			DedupeTTL:           30 * time.Minute,
			MaxAlertsPerWebhook: 20,
		},
		DeepSeek: DeepSeekConfig{
			BaseURL:    "https://api.deepseek.com",
			Model:      "deepseek-chat",
			Timeout:    45 * time.Second,
			MaxRetries: 2,
		},
		Feishu: FeishuConfig{EventMode: "webhook", APIBaseURL: "https://open.feishu.cn"},
		Kubernetes: KubernetesConfig{
			DefaultNamespace: "default",
			LogTailLines:     200,
		},
		Agent: AgentConfig{
			MemoryTTL:  12 * time.Hour,
			MaxHistory: 20,
		},
	}
}

func (c *Config) Validate() error {
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port must be between 1 and 65535")
	}
	if c.HTTP.Timeout <= 0 {
		return fmt.Errorf("http.timeout must be positive")
	}
	if c.Alertmanager.PollInterval < 0 {
		return fmt.Errorf("alertmanager.poll_interval must be zero or positive")
	}
	if c.DeepSeek.BaseURL == "" {
		return fmt.Errorf("deepseek.base_url is required")
	}
	if c.DeepSeek.Model == "" {
		return fmt.Errorf("deepseek.model is required")
	}
	if c.DeepSeek.Timeout <= 0 {
		return fmt.Errorf("deepseek.timeout must be positive")
	}
	if c.Feishu.EventMode == "" {
		c.Feishu.EventMode = "webhook"
	}
	if c.Feishu.EventMode != "webhook" && c.Feishu.EventMode != "websocket" {
		return fmt.Errorf("feishu.event_mode must be webhook or websocket")
	}
	if c.Kubernetes.DefaultNamespace == "" {
		return fmt.Errorf("kubernetes.default_namespace is required")
	}
	if c.Kubernetes.LogTailLines <= 0 {
		return fmt.Errorf("kubernetes.log_tail_lines must be positive")
	}
	if c.Agent.MemoryTTL <= 0 {
		return fmt.Errorf("agent.memory_ttl must be positive")
	}
	if c.Agent.MaxHistory <= 0 {
		return fmt.Errorf("agent.max_history must be positive")
	}
	return nil
}

func (c *Config) applyEnvOverrides() {
	if v := os.Getenv("SERVER_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			c.Server.Port = p
		}
	}
	if v := os.Getenv("ALERTMANAGER_URL"); v != "" {
		c.Alertmanager.URL = v
	}
	if v := os.Getenv("ALERTMANAGER_TOKEN"); v != "" {
		c.Alertmanager.Token = v
	}
	if v := os.Getenv("PROMETHEUS_URL"); v != "" {
		c.Prometheus.URL = v
	}
	if v := os.Getenv("PROMETHEUS_TOKEN"); v != "" {
		c.Prometheus.Token = v
	}
	if v := os.Getenv("DEEPSEEK_API_KEY"); v != "" {
		c.DeepSeek.APIKey = v
	}
	if v := os.Getenv("DEEPSEEK_BASE_URL"); v != "" {
		c.DeepSeek.BaseURL = v
	}
	if v := os.Getenv("DEEPSEEK_MODEL"); v != "" {
		c.DeepSeek.Model = v
	}
	if v := firstEnv("FEISHU_EVENT_MODE", "LARK_EVENT_MODE"); v != "" {
		c.Feishu.EventMode = v
	}
	if v := firstEnv("FEISHU_APP_ID", "LARK_APP_ID"); v != "" {
		c.Feishu.AppID = v
	}
	if v := firstEnv("FEISHU_APP_SECRET", "LARK_APP_SECRET"); v != "" {
		c.Feishu.AppSecret = v
	}
	if v := firstEnv("FEISHU_BOT_TOKEN", "LARK_BOT_TOKEN"); v != "" {
		c.Feishu.BotToken = v
	}
	if v := firstEnv("FEISHU_VERIFICATION_TOKEN", "LARK_VERIFICATION_TOKEN"); v != "" {
		c.Feishu.VerificationToken = v
	}
	if v := firstEnv("FEISHU_EVENT_SECRET", "LARK_EVENT_SECRET"); v != "" {
		c.Feishu.EventSecret = v
	}
}

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if v := os.Getenv(key); v != "" {
			return v
		}
	}
	return ""
}

func decodeConfigYAML(input string, cfg *Config) error {
	values, err := parseSimpleYAML(input)
	if err != nil {
		return err
	}
	get := func(section, key string) string {
		if values[section] == nil {
			return ""
		}
		return values[section][key]
	}
	setString := func(section, key string, dst *string) {
		if v := get(section, key); v != "" {
			*dst = v
		}
	}
	setInt := func(section, key string, dst *int) error {
		if v := get(section, key); v != "" {
			parsed, err := strconv.Atoi(v)
			if err != nil {
				return fmt.Errorf("%s.%s must be integer: %w", section, key, err)
			}
			*dst = parsed
		}
		return nil
	}
	setDuration := func(section, key string, dst *time.Duration) error {
		if v := get(section, key); v != "" {
			parsed, err := time.ParseDuration(v)
			if err != nil {
				return fmt.Errorf("%s.%s must be duration: %w", section, key, err)
			}
			*dst = parsed
		}
		return nil
	}

	if err := setInt("server", "port", &cfg.Server.Port); err != nil {
		return err
	}
	setString("log", "level", &cfg.Log.Level)
	if err := setDuration("http", "timeout", &cfg.HTTP.Timeout); err != nil {
		return err
	}
	setString("alertmanager", "url", &cfg.Alertmanager.URL)
	setString("alertmanager", "token", &cfg.Alertmanager.Token)
	if err := setDuration("alertmanager", "poll_interval", &cfg.Alertmanager.PollInterval); err != nil {
		return err
	}
	if err := setDuration("alertmanager", "dedupe_ttl", &cfg.Alertmanager.DedupeTTL); err != nil {
		return err
	}
	if err := setInt("alertmanager", "max_alerts_per_webhook", &cfg.Alertmanager.MaxAlertsPerWebhook); err != nil {
		return err
	}
	setString("prometheus", "url", &cfg.Prometheus.URL)
	setString("prometheus", "token", &cfg.Prometheus.Token)
	setString("deepseek", "api_key", &cfg.DeepSeek.APIKey)
	setString("deepseek", "base_url", &cfg.DeepSeek.BaseURL)
	setString("deepseek", "model", &cfg.DeepSeek.Model)
	if err := setDuration("deepseek", "timeout", &cfg.DeepSeek.Timeout); err != nil {
		return err
	}
	if err := setInt("deepseek", "max_retries", &cfg.DeepSeek.MaxRetries); err != nil {
		return err
	}
	setString("feishu", "event_mode", &cfg.Feishu.EventMode)
	setString("feishu", "app_id", &cfg.Feishu.AppID)
	setString("feishu", "app_secret", &cfg.Feishu.AppSecret)
	setString("feishu", "bot_token", &cfg.Feishu.BotToken)
	setString("feishu", "verification_token", &cfg.Feishu.VerificationToken)
	setString("feishu", "event_secret", &cfg.Feishu.EventSecret)
	setString("feishu", "api_base_url", &cfg.Feishu.APIBaseURL)
	setString("kubernetes", "kubeconfig", &cfg.Kubernetes.Kubeconfig)
	setString("kubernetes", "default_namespace", &cfg.Kubernetes.DefaultNamespace)
	if err := setInt("kubernetes", "log_tail_lines", &cfg.Kubernetes.LogTailLines); err != nil {
		return err
	}
	if err := setDuration("agent", "memory_ttl", &cfg.Agent.MemoryTTL); err != nil {
		return err
	}
	if err := setInt("agent", "max_history", &cfg.Agent.MaxHistory); err != nil {
		return err
	}
	return nil
}

func parseSimpleYAML(input string) (map[string]map[string]string, error) {
	values := make(map[string]map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(input))
	currentSection := ""
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := stripComment(scanner.Text())
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		trimmed := strings.TrimSpace(line)
		if indent == 0 {
			if !strings.HasSuffix(trimmed, ":") {
				return nil, fmt.Errorf("line %d: expected section", lineNo)
			}
			currentSection = strings.TrimSuffix(trimmed, ":")
			values[currentSection] = make(map[string]string)
			continue
		}
		if currentSection == "" {
			return nil, fmt.Errorf("line %d: key without section", lineNo)
		}
		parts := strings.SplitN(trimmed, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("line %d: expected key: value", lineNo)
		}
		key := strings.TrimSpace(parts[0])
		value := normalizeYAMLScalar(strings.TrimSpace(parts[1]))
		values[currentSection][key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func stripComment(line string) string {
	inSingle := false
	inDouble := false
	for i, r := range line {
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble {
				return line[:i]
			}
		}
	}
	return line
}

func normalizeYAMLScalar(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
			return value[1 : len(value)-1]
		}
	}
	return value
}

var envPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

func expandEnv(input string) string {
	return envPattern.ReplaceAllStringFunc(input, func(match string) string {
		key := envPattern.FindStringSubmatch(match)[1]
		return os.Getenv(key)
	})
}
