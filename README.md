# AI-SRE Agent

AI-SRE Agent 是一个 Go 实现的企业级智能运维助手，接入 Alertmanager、飞书 Bot、DeepSeek 兼容大模型、Prometheus 和 kubectl，提供告警自动分析、自然语言排障和自动化查询能力。

## 目录结构

```text
.
├── cmd/ai-sre-agent/main.go
├── config.yaml
├── Dockerfile
├── go.mod
├── internal
│   ├── agent
│   ├── alertmanager
│   ├── config
│   ├── feishu
│   ├── llm
│   ├── memory
│   ├── server
│   └── tools
└── pkg
    └── logx
```

## 核心能力

- Alertmanager webhook：`POST /api/alert/webhook`
- Alertmanager active alerts 查询：`GET /api/alert/active`
- Alertmanager API 兼容拉取：配置 `alertmanager.poll_interval` 大于 `0s` 后定时分析 active 告警
- 飞书事件回调：`POST /api/feishu/events`
- API 聊天入口：`POST /api/chat`
- DeepSeek ChatCompletion 封装，支持 system prompt、工具 schema、超时和重试
- Agent Tool Calling：active alerts、pods、describe pod、pod logs、nodes、Prometheus query
- 内存会话上下文：支持多轮问答
- 告警去重：按 fingerprint 或关键 label 在 TTL 内收敛

## 快速启动

```bash
export DEEPSEEK_API_KEY="sk-xxx"
go run ./cmd/ai-sre-agent -config config.yaml
```

健康检查：

```bash
curl http://127.0.0.1:8080/healthz
```

自然语言调用：

```bash
curl -X POST http://127.0.0.1:8080/api/chat \
  -H 'Content-Type: application/json' \
  -d '{"session_id":"local","text":"为什么 payment pod 一直重启"}'
```

## Alertmanager 配置示例

```yaml
receivers:
  - name: ai-sre-agent
    webhook_configs:
      - url: http://ai-sre-agent.default.svc:8080/api/alert/webhook
        send_resolved: true
```

## 飞书配置

在 `config.yaml` 中配置：

```yaml
feishu:
  event_mode: websocket
  app_id: cli_a93890863bb81cd6
  app_secret: qMugZMRqmYuIDcgaBW5UNd40lrpeFJXs
  bot_token: https://open.feishu.cn/open-apis/bot/v2/hook/9e2b50a2-7749-4a1a-8644-eeebf134825a
  verification_token: xxx
  event_secret: xxx
```

说明：

- `event_mode` 支持 `webhook` 和 `websocket`。没有公网地址时使用 `websocket` 长连接模式。
- `bot_token` 用于群机器人 webhook 主动推送告警分析。
- `app_id/app_secret` 用于获取 tenant access token 并回复用户消息。
- `verification_token` 用于事件 token 校验。
- `event_secret` 用于事件签名校验；为空时跳过签名校验，生产环境建议配置。

如果目前只有飞书应用的 `app_id/app_secret`，可以先只配置这两个来支持“收到飞书事件后回复消息”。但主动把 Alertmanager 告警分析推送到群里，需要额外配置群机器人的 `bot_token`，否则服务会跳过主动推送。

没有公网地址时，在飞书开放平台的事件订阅里选择「使用长连接接收事件」，并订阅 `im.message.receive_v1`。服务启动后会主动连接飞书，不需要配置公网回调 URL。

长连接模式排查要点：

- 启动日志应出现 `starting feishu websocket event receiver`。
- 飞书开放平台事件订阅方式必须选择「使用长连接接收事件」。
- 应用必须开启「机器人」能力，并发布/启用。
- 应用机器人需要被拉进群，群里需要 `@机器人` 发送文本消息。
- 事件必须订阅 `im.message.receive_v1`。
- 不要开启事件加密；当前长连接接收器按明文事件处理。
- 服务所在机器需要能访问飞书开放平台公网。

环境变量也支持飞书/Lark 两套命名：

```bash
export LARK_EVENT_MODE="websocket"
export LARK_APP_ID="cli_xxx"
export LARK_APP_SECRET="xxx"
# 可选
export LARK_BOT_TOKEN="xxx"
export LARK_VERIFICATION_TOKEN="xxx"
export LARK_EVENT_SECRET="xxx"
```

## Kubernetes 权限

运行环境需要能执行 `kubectl`，并具备以下最小权限：

```yaml
rules:
  - apiGroups: [""]
    resources: ["pods", "pods/log", "nodes"]
    verbs: ["get", "list"]
```

## Docker

```bash
docker build -t ai-sre-agent:latest .
docker run --rm -p 8080:8080 \
  -e DEEPSEEK_API_KEY="sk-xxx" \
  ai-sre-agent:latest
```

在 Kubernetes 中部署时，建议通过 ConfigMap 挂载 `config.yaml`，通过 Secret 注入 DeepSeek、Feishu、Alertmanager、Prometheus token。

## API 示例

查询 active 告警：

```bash
curl http://127.0.0.1:8080/api/alert/active
```

模拟聊天：

```bash
curl -X POST http://127.0.0.1:8080/api/chat \
  -H 'Content-Type: application/json' \
  -d '{"session_id":"sre","text":"查看 mysql CPU 高的原因"}'
```

## 生产建议

- 将 `kubectl` 查询迁移为 Kubernetes client-go，可获得更好的权限控制和结构化结果。
- 将内存会话迁移到 Redis 或 Postgres，支持多副本横向扩展。
- 为 `/api/alert/webhook` 增加来源鉴权或内网网关访问控制。
- 为 Prometheus 查询增加 allowlist，避免任意高成本 PromQL。
- 将告警分析结果和用户问答写入审计日志，便于复盘。
