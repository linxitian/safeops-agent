package executor

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"safeops-agent/contracts"
)

type HTTPHandler struct {
	Executor Executor
	Mode     ExecutionMode
}

func (h HTTPHandler) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		mode := h.Mode
		if mode == "" {
			mode = DryRun
		}
		writeExecutorJSON(w, http.StatusOK, map[string]any{"status": "ok", "transport": "unix", "execution_mode": mode})
	})
	mux.HandleFunc("POST /v1/execute", h.execute)
	return mux
}
func (h HTTPHandler) execute(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var envelope contracts.ActionEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		writeExecutorJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeExecutorJSON(w, http.StatusBadRequest, map[string]any{"error": "body must contain one ActionEnvelope"})
		return
	}
	result, err := h.Executor.Execute(r.Context(), envelope)
	if err != nil {
		writeExecutorJSON(w, http.StatusForbidden, map[string]any{"error": err.Error()})
		return
	}
	writeExecutorJSON(w, http.StatusOK, result)
}
func writeExecutorJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
