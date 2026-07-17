package api

import (
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"
)

type runtimeLog struct {
	mu     sync.Mutex
	writer io.Writer
}

type runtimeLogEntry struct {
	Timestamp  time.Time `json:"timestamp"`
	Method     string    `json:"method"`
	Path       string    `json:"path"`
	Status     int       `json:"status"`
	Bytes      int       `json:"bytes"`
	DurationMS int64     `json:"duration_ms"`
}

type loggingResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *loggingResponseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *loggingResponseWriter) Write(value []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(value)
	w.bytes += n
	return n, err
}

func (w *loggingResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (l *runtimeLog) handler(next http.Handler) http.Handler {
	if l == nil || l.writer == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now().UTC()
		recorder := &loggingResponseWriter{ResponseWriter: w}
		next.ServeHTTP(recorder, r)
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		l.write(runtimeLogEntry{Timestamp: started, Method: r.Method, Path: r.URL.Path, Status: status, Bytes: recorder.bytes, DurationMS: time.Since(started).Milliseconds()})
	})
}

func (l *runtimeLog) write(entry runtimeLogEntry) {
	line, err := json.Marshal(entry)
	if err != nil {
		return
	}
	line = append(line, '\n')
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.writer.Write(line)
	if syncer, ok := l.writer.(interface{ Sync() error }); ok {
		_ = syncer.Sync()
	}
}
