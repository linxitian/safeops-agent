package trace

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Type string

const (
	Received           Type = "RECEIVED"
	TaskResumed        Type = "TASK_RESUMED"
	IntentParsed       Type = "INTENT_PARSED"
	TaskCreated        Type = "TASK_CREATED"
	PlanCreated        Type = "PLAN_CREATED"
	DecisionRecorded   Type = "DECISION_RECORDED"
	ActionProposed     Type = "ACTION_PROPOSED"
	StaticGuardResult  Type = "STATIC_GUARD_RESULT"
	IntentGuardResult  Type = "INTENT_GUARD_RESULT"
	RiskEvaluated      Type = "RISK_EVALUATED"
	ApprovalRequested  Type = "APPROVAL_REQUESTED"
	ApprovalResult     Type = "APPROVAL_RESULT"
	ApprovalResolved   Type = ApprovalResult
	ToolCall           Type = "TOOL_CALL"
	ToolResult         Type = "TOOL_RESULT"
	Execution          Type = "EXECUTION"
	ExecutionStarted   Type = Execution
	ExecutionFinished  Type = Execution
	Verification       Type = "VERIFICATION"
	VerificationResult Type = Verification
	Rollback           Type = "ROLLBACK"
	RollbackStarted    Type = Rollback
	RollbackFinished   Type = Rollback
	FindingsUpdated    Type = "FINDINGS_UPDATED"
	KnowledgeRetrieved Type = "KNOWLEDGE_RETRIEVED"
	RCAResult          Type = "RCA_RESULT"
	TaskCompleted      Type = "TASK_COMPLETED"
	TaskFailed         Type = "TASK_FAILED"
	TaskCancelled      Type = "TASK_CANCELLED"
	Final              Type = "FINAL"
)

type Event struct {
	Sequence  uint64          `json:"sequence"`
	Timestamp time.Time       `json:"timestamp"`
	Type      Type            `json:"type"`
	TaskID    string          `json:"task_id"`
	SessionID string          `json:"session_id"`
	Data      json.RawMessage `json:"data"`
	PrevHash  string          `json:"prev_hash"`
	EventHash string          `json:"event_hash"`
}

// DecisionRecord is an auditable decision summary, never hidden model
// chain-of-thought. Append normalizes every DECISION_RECORDED payload to this
// field set while retaining safe workflow-specific fields.
type DecisionRecord struct {
	DecisionID           string `json:"decision_id"`
	TaskID               string `json:"task_id"`
	PlanVersion          int    `json:"plan_version"`
	Objective            any    `json:"objective"`
	CurrentStep          any    `json:"current_step"`
	DecisionSummary      any    `json:"decision_summary"`
	CandidateHypotheses  any    `json:"candidate_hypotheses"`
	SelectedHypothesis   any    `json:"selected_hypothesis"`
	RejectedHypotheses   any    `json:"rejected_hypotheses"`
	EvidenceUsed         any    `json:"evidence_used"`
	EvidenceMissing      any    `json:"evidence_missing"`
	SelectedTool         any    `json:"selected_tool"`
	ToolArgumentsDigest  any    `json:"tool_arguments_digest"`
	ExpectedObservation  any    `json:"expected_observation"`
	CompletionAssessment any    `json:"completion_assessment"`
	ReplanReason         any    `json:"replan_reason"`
}

type canonicalEvent struct {
	Sequence  uint64          `json:"sequence"`
	Timestamp time.Time       `json:"timestamp"`
	Type      Type            `json:"type"`
	TaskID    string          `json:"task_id"`
	SessionID string          `json:"session_id"`
	Data      json.RawMessage `json:"data"`
}
type head struct {
	Count uint64 `json:"count"`
	Hash  string `json:"hash"`
}

type Writer struct {
	dir string
	mu  sync.Mutex
}

func NewWriter(dir string) (*Writer, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	w := &Writer{dir: dir}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	taskIDs := map[string]bool{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		switch {
		case strings.HasSuffix(name, ".jsonl"):
			taskIDs[strings.TrimSuffix(name, ".jsonl")] = true
		case strings.HasSuffix(name, ".head.json"):
			taskIDs[strings.TrimSuffix(name, ".head.json")] = true
		}
	}
	for taskID := range taskIDs {
		if err := validID(taskID); err != nil {
			return nil, err
		}
		if _, err := w.recoverLocked(taskID); err != nil {
			return nil, fmt.Errorf("recover trace %s: %w", taskID, err)
		}
	}
	return w, nil
}

func (w *Writer) Append(ctx context.Context, taskID, sessionID string, typ Type, data any) (Event, error) {
	if err := ctx.Err(); err != nil {
		return Event{}, err
	}
	if err := validID(taskID); err != nil {
		return Event{}, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	h, err := w.recoverLocked(taskID)
	if err != nil {
		return Event{}, err
	}
	if _, err := os.Stat(w.headPath(taskID)); errors.Is(err, os.ErrNotExist) {
		if err := atomicJSON(w.headPath(taskID), head{}); err != nil {
			return Event{}, err
		}
	} else if err != nil {
		return Event{}, err
	}
	payload, err := marshalPayload(taskID, h.Count+1, typ, data)
	if err != nil {
		return Event{}, err
	}
	ce := canonicalEvent{Sequence: h.Count + 1, Timestamp: time.Now().UTC(), Type: typ, TaskID: taskID, SessionID: sessionID, Data: payload}
	canonical, err := json.Marshal(ce)
	if err != nil {
		return Event{}, err
	}
	sum := sha256.Sum256(append([]byte(h.Hash), canonical...))
	event := Event{Sequence: ce.Sequence, Timestamp: ce.Timestamp, Type: ce.Type, TaskID: taskID, SessionID: sessionID, Data: payload, PrevHash: h.Hash, EventHash: hex.EncodeToString(sum[:])}
	line, err := json.Marshal(event)
	if err != nil {
		return Event{}, err
	}
	f, err := os.OpenFile(w.tracePath(taskID), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return Event{}, err
	}
	encodedLine := append(line, '\n')
	if n, err := f.Write(encodedLine); err != nil {
		f.Close()
		return Event{}, err
	} else if n != len(encodedLine) {
		f.Close()
		return Event{}, io.ErrShortWrite
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return Event{}, err
	}
	if err := f.Close(); err != nil {
		return Event{}, err
	}
	if err := syncDir(w.dir); err != nil {
		return Event{}, err
	}
	if err := atomicJSON(w.headPath(taskID), head{Count: event.Sequence, Hash: event.EventHash}); err != nil {
		return Event{}, err
	}
	return event, nil
}

func (w *Writer) VerifyIntegrity(taskID string) error {
	if err := validID(taskID); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	events, hashes, err := w.readEvents(taskID)
	if err != nil {
		return err
	}
	expectedHead, err := w.readHead(taskID)
	if err != nil {
		return err
	}
	lastHash := ""
	if len(hashes) > 0 {
		lastHash = hashes[len(hashes)-1]
	}
	if expectedHead.Count != uint64(len(events)) || expectedHead.Hash != lastHash {
		return errors.New("trace head does not match event chain; an event may have been deleted or reordered")
	}
	return nil
}

func (w *Writer) Read(taskID string) ([]Event, error) {
	if err := validID(taskID); err != nil {
		return nil, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	events, _, err := w.readEvents(taskID)
	return events, err
}

func (w *Writer) recoverLocked(taskID string) (head, error) {
	h, err := w.readHead(taskID)
	if err != nil {
		return head{}, err
	}
	data, err := os.ReadFile(w.tracePath(taskID))
	if errors.Is(err, os.ErrNotExist) {
		if h.Count != 0 || h.Hash != "" {
			return head{}, errors.New("trace head exists but event data is missing")
		}
		return h, nil
	}
	if err != nil {
		return head{}, err
	}
	needsTruncate := false
	truncateAt := len(data)
	if len(data) > 0 && data[len(data)-1] != '\n' {
		cut := bytes.LastIndexByte(data, '\n') + 1
		needsTruncate = true
		truncateAt = cut
		data = data[:cut]
	}
	events, hashes, err := parseEvents(taskID, data)
	if err != nil {
		return head{}, err
	}
	if h.Count > uint64(len(events)) {
		return head{}, errors.New("trace event data is shorter than the committed head")
	}
	if h.Count == 0 {
		if h.Hash != "" {
			return head{}, errors.New("zero trace head has a non-empty hash")
		}
	} else if hashes[h.Count-1] != h.Hash {
		return head{}, errors.New("committed trace prefix does not match the head")
	}
	if needsTruncate {
		if err := truncateAndSync(w.tracePath(taskID), int64(truncateAt)); err != nil {
			return head{}, err
		}
	}
	if uint64(len(events)) > h.Count {
		h = head{Count: uint64(len(events)), Hash: hashes[len(hashes)-1]}
		if err := atomicJSON(w.headPath(taskID), h); err != nil {
			return head{}, err
		}
	}
	return h, nil
}

func (w *Writer) readEvents(taskID string) ([]Event, []string, error) {
	data, err := os.ReadFile(w.tracePath(taskID))
	if err != nil {
		return nil, nil, err
	}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		return nil, nil, errors.New("trace has an incomplete crash tail")
	}
	return parseEvents(taskID, data)
}

func parseEvents(taskID string, data []byte) ([]Event, []string, error) {
	if len(data) == 0 {
		return nil, nil, nil
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var events []Event
	var hashes []string
	previous := ""
	for scanner.Scan() {
		sequence := uint64(len(events) + 1)
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, nil, fmt.Errorf("event %d decode: %w", sequence, err)
		}
		if event.Sequence != sequence {
			return nil, nil, fmt.Errorf("event sequence mismatch at %d", sequence)
		}
		if event.TaskID != taskID {
			return nil, nil, fmt.Errorf("event %d task mismatch", sequence)
		}
		if event.PrevHash != previous {
			return nil, nil, fmt.Errorf("event %d previous hash mismatch", sequence)
		}
		ce := canonicalEvent{Sequence: event.Sequence, Timestamp: event.Timestamp, Type: event.Type, TaskID: event.TaskID, SessionID: event.SessionID, Data: event.Data}
		canonical, err := json.Marshal(ce)
		if err != nil {
			return nil, nil, err
		}
		sum := sha256.Sum256(append([]byte(previous), canonical...))
		expected := hex.EncodeToString(sum[:])
		if event.EventHash != expected {
			return nil, nil, fmt.Errorf("event %d hash mismatch", sequence)
		}
		events = append(events, event)
		hashes = append(hashes, expected)
		previous = expected
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, err
	}
	return events, hashes, nil
}

func (w *Writer) readHead(taskID string) (head, error) {
	b, err := os.ReadFile(w.headPath(taskID))
	if errors.Is(err, os.ErrNotExist) {
		if info, traceErr := os.Stat(w.tracePath(taskID)); traceErr == nil && info.Size() > 0 {
			return head{}, errors.New("trace head is missing while event data exists")
		} else if traceErr != nil && !errors.Is(traceErr, os.ErrNotExist) {
			return head{}, traceErr
		}
		return head{}, nil
	}
	if err != nil {
		return head{}, err
	}
	var out head
	if err := json.Unmarshal(b, &out); err != nil {
		return head{}, err
	}
	return out, nil
}
func (w *Writer) tracePath(id string) string { return filepath.Join(w.dir, id+".jsonl") }
func (w *Writer) headPath(id string) string  { return filepath.Join(w.dir, id+".head.json") }
func validID(id string) error {
	if id == "" || strings.ContainsAny(id, `/\\`) || id == "." || id == ".." {
		return fmt.Errorf("invalid trace id %q", id)
	}
	return nil
}

var (
	bearerPattern     = regexp.MustCompile(`(?i)\bbearer\s+[a-z0-9._~+/=-]+`)
	openAIKeyPattern  = regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{8,}\b`)
	assignmentPattern = regexp.MustCompile(`(?i)\b(api[_-]?key|access[_-]?token|password|passwd|secret|authorization)\s*[:=]\s*[^\s,;]+`)
)

func marshalPayload(taskID string, sequence uint64, typ Type, data any) (json.RawMessage, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	value = redactValue(value)
	if typ == DecisionRecorded {
		value = normalizeDecision(taskID, sequence, value)
	}
	return json.Marshal(value)
}

func normalizeDecision(taskID string, sequence uint64, value any) map[string]any {
	record, ok := value.(map[string]any)
	if !ok {
		record = map[string]any{"decision_summary": "structured workflow decision"}
	}
	record["decision_id"] = fmt.Sprintf("decision_%s_%d", taskID, sequence)
	record["task_id"] = taskID
	defaults := map[string]any{
		"plan_version":          1,
		"objective":             "",
		"current_step":          "",
		"decision_summary":      "structured workflow decision",
		"candidate_hypotheses":  []any{},
		"selected_hypothesis":   "",
		"rejected_hypotheses":   []any{},
		"evidence_used":         []any{},
		"evidence_missing":      []any{},
		"selected_tool":         "",
		"tool_arguments_digest": "",
		"expected_observation":  "",
		"completion_assessment": "",
		"replan_reason":         "",
	}
	for key, fallback := range defaults {
		if _, exists := record[key]; !exists {
			record[key] = fallback
		}
	}
	for _, key := range []string{"tool_arguments", "arguments"} {
		arguments, exists := record[key]
		if !exists {
			continue
		}
		encoded, err := json.Marshal(arguments)
		if err == nil {
			digest := sha256.Sum256(encoded)
			record["tool_arguments_digest"] = hex.EncodeToString(digest[:])
		}
		delete(record, key)
	}
	return record
}

func redactValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			if sensitiveKey(key) {
				out[key] = "[REDACTED]"
				continue
			}
			out[key] = redactValue(child)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for index, child := range typed {
			out[index] = redactValue(child)
		}
		return out
	case string:
		redacted := bearerPattern.ReplaceAllString(typed, "Bearer [REDACTED]")
		redacted = openAIKeyPattern.ReplaceAllString(redacted, "[REDACTED]")
		return assignmentPattern.ReplaceAllStringFunc(redacted, func(match string) string {
			separator := strings.IndexAny(match, ":=")
			if separator < 0 {
				return "[REDACTED]"
			}
			return strings.TrimSpace(match[:separator]) + "=[REDACTED]"
		})
	default:
		return value
	}
}

func sensitiveKey(key string) bool {
	normalized := strings.ToLower(key)
	replacer := strings.NewReplacer("_", "", "-", "", ".", "", " ", "")
	normalized = replacer.Replace(normalized)
	switch normalized {
	case "token", "accesstoken", "refreshtoken", "idtoken", "authorization", "cookie", "setcookie", "apikey", "password", "passwd", "secret", "clientsecret", "chainofthought", "hiddenreasoning", "reasoningcontent", "cot":
		return true
	}
	return strings.Contains(normalized, "password") || strings.Contains(normalized, "secret") || strings.HasSuffix(normalized, "token")
}

func truncateAndSync(path string, size int64) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	if err := f.Truncate(size); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

func syncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func atomicJSON(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".trace-head-*.tmp")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	return syncDir(dir)
}
