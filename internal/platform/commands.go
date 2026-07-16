package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type CommandPaths struct {
	Systemctl  string
	Journalctl string
	SS         string
	IP         string
}

func DiscoverCommandPaths() CommandPaths {
	return CommandPaths{Systemctl: FindFixedBinary("systemctl"), Journalctl: FindFixedBinary("journalctl"), SS: FindFixedBinary("ss"), IP: FindFixedBinary("ip")}
}

var binaryNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.+-]{1,64}$`)

// FindFixedBinary resolves a developer-authored binary name without accepting
// a path or model-provided command string. All callers still pass individually
// fixed arguments to OSFixedRunner.
func FindFixedBinary(name string) string {
	if !binaryNamePattern.MatchString(name) {
		return ""
	}
	path, _ := exec.LookPath(name)
	return path
}

type FixedRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}
type OSFixedRunner struct{ MaxOutputBytes int }

func (r OSFixedRunner) Run(ctx context.Context, binary string, args ...string) ([]byte, error) {
	if binary == "" {
		return nil, errors.New("required fixed binary is unavailable")
	}
	limit := r.MaxOutputBytes
	if limit <= 0 {
		limit = 4 << 20
	}
	stdout := &limitedBuffer{limit: limit}
	stderr := &limitedBuffer{limit: 64 << 10}
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("fixed command %s failed: %w: %s", binary, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

type limitedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	remaining := b.limit - b.Len()
	if remaining <= 0 {
		return 0, errors.New("command output limit exceeded")
	}
	if len(p) > remaining {
		_, _ = b.Buffer.Write(p[:remaining])
		return remaining, errors.New("command output limit exceeded")
	}
	return b.Buffer.Write(p)
}

type CommandPlatform struct {
	Paths  CommandPaths
	Runner FixedRunner
}

func NewCommandPlatform() *CommandPlatform {
	return &CommandPlatform{Paths: DiscoverCommandPaths(), Runner: OSFixedRunner{}}
}
func NewCommandPlatformWithRunner(paths CommandPaths, runner FixedRunner) *CommandPlatform {
	return &CommandPlatform{Paths: paths, Runner: runner}
}

type ServiceStatus struct {
	Name           string `json:"name"`
	Description    string `json:"description"`
	LoadState      string `json:"load_state"`
	ActiveState    string `json:"active_state"`
	SubState       string `json:"sub_state"`
	MainPID        int    `json:"main_pid"`
	ExecMainStatus int    `json:"exec_main_status"`
	RestartCount   uint64 `json:"restart_count"`
	FragmentPath   string `json:"fragment_path,omitempty"`
}
type ServiceDependencies struct {
	Name     string   `json:"name"`
	Requires []string `json:"requires"`
	Wants    []string `json:"wants"`
	After    []string `json:"after"`
	Before   []string `json:"before"`
}

var unitPattern = regexp.MustCompile(`^[A-Za-z0-9_.@:-]{1,255}$`)

func validateUnit(unit string) (string, error) {
	unit = strings.TrimSpace(unit)
	if !unitPattern.MatchString(unit) {
		return "", errors.New("invalid systemd unit")
	}
	if !strings.Contains(unit, ".") {
		unit += ".service"
	}
	return unit, nil
}

func (p *CommandPlatform) Service(ctx context.Context, unit string) (ServiceStatus, error) {
	unit, err := validateUnit(unit)
	if err != nil {
		return ServiceStatus{}, err
	}
	properties := []string{"Id", "Description", "LoadState", "ActiveState", "SubState", "MainPID", "ExecMainStatus", "NRestarts", "FragmentPath"}
	output, err := p.Runner.Run(ctx, p.Paths.Systemctl, "show", "--no-pager", "--property="+strings.Join(properties, ","), unit)
	if err != nil {
		return ServiceStatus{}, err
	}
	values := parseKeyValues(output)
	return serviceFromValues(unit, values), nil
}
func serviceFromValues(unit string, values map[string]string) ServiceStatus {
	return ServiceStatus{Name: firstNonEmpty(values["Id"], unit), Description: values["Description"], LoadState: values["LoadState"], ActiveState: values["ActiveState"], SubState: values["SubState"], MainPID: parseInt(values["MainPID"]), ExecMainStatus: parseInt(values["ExecMainStatus"]), RestartCount: parseUint(values["NRestarts"]), FragmentPath: values["FragmentPath"]}
}
func (p *CommandPlatform) FailedServices(ctx context.Context) ([]ServiceStatus, error) {
	output, err := p.Runner.Run(ctx, p.Paths.Systemctl, "list-units", "--type=service", "--state=failed", "--no-legend", "--no-pager", "--plain")
	if err != nil {
		return nil, err
	}
	var out []ServiceStatus
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		description := ""
		if len(fields) > 4 {
			description = strings.Join(fields[4:], " ")
		}
		out = append(out, ServiceStatus{Name: strings.TrimPrefix(fields[0], "●"), LoadState: fields[1], ActiveState: fields[2], SubState: fields[3], Description: description})
	}
	return out, nil
}
func (p *CommandPlatform) ServiceDependencies(ctx context.Context, unit string) (ServiceDependencies, error) {
	unit, err := validateUnit(unit)
	if err != nil {
		return ServiceDependencies{}, err
	}
	output, err := p.Runner.Run(ctx, p.Paths.Systemctl, "show", "--no-pager", "--property=Id,Requires,Wants,After,Before", unit)
	if err != nil {
		return ServiceDependencies{}, err
	}
	values := parseKeyValues(output)
	return ServiceDependencies{Name: firstNonEmpty(values["Id"], unit), Requires: strings.Fields(values["Requires"]), Wants: strings.Fields(values["Wants"]), After: strings.Fields(values["After"]), Before: strings.Fields(values["Before"])}, nil
}
func parseKeyValues(output []byte) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(string(output), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			out[key] = value
		}
	}
	return out
}
func parseInt(value string) int     { v, _ := strconv.Atoi(value); return v }
func parseUint(value string) uint64 { v, _ := strconv.ParseUint(value, 10, 64); return v }
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

type JournalQuery struct {
	Unit     string
	Lines    int
	Priority int
}
type JournalEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Unit      string    `json:"unit,omitempty"`
	PID       int       `json:"pid,omitempty"`
	Priority  int       `json:"priority"`
	Message   string    `json:"message"`
	BootID    string    `json:"boot_id,omitempty"`
	Cursor    string    `json:"cursor,omitempty"`
	Redacted  bool      `json:"redacted"`
}

func (p *CommandPlatform) Journal(ctx context.Context, query JournalQuery) ([]JournalEvent, error) {
	lines := query.Lines
	if lines <= 0 {
		lines = 100
	}
	if lines > 500 {
		return nil, errors.New("journal lines must not exceed 500")
	}
	args := []string{"--no-pager", "--output=json", "--lines=" + strconv.Itoa(lines)}
	if query.Unit != "" {
		unit, err := validateUnit(query.Unit)
		if err != nil {
			return nil, err
		}
		args = append(args, "--unit="+unit)
	}
	if query.Priority >= 0 {
		if query.Priority > 7 {
			return nil, errors.New("journal priority must be from 0 to 7")
		}
		args = append(args, "--priority="+strconv.Itoa(query.Priority))
	}
	output, err := p.Runner.Run(ctx, p.Paths.Journalctl, args...)
	if err != nil {
		return nil, err
	}
	return parseJournal(output), nil
}

func parseJournal(output []byte) []JournalEvent {
	var out []JournalEvent
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		values := map[string]any{}
		if err := json.Unmarshal([]byte(line), &values); err != nil {
			continue
		}
		message := jsonString(values["MESSAGE"])
		if len(message) > 4096 {
			message = message[:4096] + "…"
		}
		redacted := redactCommand([]byte(message))
		out = append(out, JournalEvent{Timestamp: journalTime(jsonString(values["_SOURCE_REALTIME_TIMESTAMP"])), Unit: jsonString(values["_SYSTEMD_UNIT"]), PID: parseInt(jsonString(values["_PID"])), Priority: parseInt(jsonString(values["PRIORITY"])), Message: redacted, BootID: jsonString(values["_BOOT_ID"]), Cursor: jsonString(values["__CURSOR"]), Redacted: redacted != message})
	}
	return out
}
func journalTime(value string) time.Time {
	micros, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(0, micros*int64(time.Microsecond)).UTC()
}
func jsonString(value any) string {
	switch value := value.(type) {
	case string:
		return value
	case []any:
		var parts []string
		for _, item := range value {
			parts = append(parts, fmt.Sprint(item))
		}
		return strings.Join(parts, " ")
	default:
		if value == nil {
			return ""
		}
		return fmt.Sprint(value)
	}
}
