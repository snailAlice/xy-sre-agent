FROM golang:1.23-alpine AS builder

WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
COPY pkg ./pkg

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/ai-sre-agent ./cmd/ai-sre-agent

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata kubectl

WORKDIR /app
COPY --from=builder /out/ai-sre-agent /app/ai-sre-agent
COPY config.yaml /app/config.yaml

EXPOSE 8080
ENTRYPOINT ["/app/ai-sre-agent"]
CMD ["-config", "/app/config.yaml"]
