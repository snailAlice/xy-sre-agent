package server

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"xy-sre-agent/internal/alertmanager"
)

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) alertWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read alertmanager webhook payload failed")
		return
	}
	payload, err := alertmanager.ParseWebhookPayload(body)
	if err != nil {
		s.logger.Warn("invalid alertmanager webhook payload", "error", err)
		writeError(w, http.StatusBadRequest, "invalid alertmanager webhook payload")
		return
	}
	if err := s.alertProcessor.ProcessWebhook(r.Context(), payload); err != nil {
		s.logger.Error("process alert webhook failed", "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) activeAlerts(w http.ResponseWriter, r *http.Request) {
	alerts, err := s.alertClient.GetActiveAlerts(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"alerts": alerts, "count": len(alerts)})
}

func (s *Server) chat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string `json:"session_id"`
		Text      string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid chat payload")
		return
	}
	if req.SessionID == "" {
		req.SessionID = "api"
	}
	answer, err := s.agent.HandleMessage(r.Context(), req.SessionID, req.Text)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"answer": answer})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func requestLogger(logger interface {
	Info(msg string, args ...any)
}, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		logger.Info("http request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(start).String())
	})
}
