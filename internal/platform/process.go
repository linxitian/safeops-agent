package platform

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type ProcessInfo struct {
	PID        int    `json:"pid"`
	PPID       int    `json:"ppid"`
	Name       string `json:"name"`
	State      string `json:"state"`
	UID        int    `json:"uid"`
	Command    string `json:"command"`
	Executable string `json:"executable,omitempty"`
	StartTicks uint64 `json:"start_ticks"`
	CPUTicks   uint64 `json:"cpu_ticks"`
	RSSBytes   uint64 `json:"rss_bytes"`
}

type ProcessQuery struct {
	Search string
	Limit  int
	SortBy string
}

type ProcessIdentity struct {
	PID           int
	StartTicks    uint64
	CommandDigest string
	UID           int
	Executable    string
}

type ProcessExecutableFallback func(context.Context, int, int) (string, error)

func (p *LinuxPlatform) ProcessIdentity(ctx context.Context, pid int) (ProcessIdentity, error) {
	info, err := p.Process(ctx, pid)
	if err != nil {
		return ProcessIdentity{}, err
	}
	executable, err := resolveProcessExecutable(ctx, filepath.Join(p.procRoot, strconv.Itoa(pid), "exe"), pid, info.UID, os.Readlink, p.processExecutableFallback)
	if err != nil {
		return ProcessIdentity{}, err
	}
	raw, err := p.readContext(ctx, filepath.Join(p.procRoot, strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return ProcessIdentity{}, err
	}
	sum := sha256.Sum256(raw)
	return ProcessIdentity{PID: pid, StartTicks: info.StartTicks, CommandDigest: hex.EncodeToString(sum[:]), UID: info.UID, Executable: executable}, nil
}

func resolveProcessExecutable(ctx context.Context, path string, pid, uid int, readlink func(string) (string, error), fallback ProcessExecutableFallback) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	executable, err := readlink(path)
	if err == nil {
		return validateProcessExecutable(executable)
	}
	if !errors.Is(err, os.ErrPermission) || fallback == nil {
		return "", fmt.Errorf("read process executable: %w", err)
	}
	executable, err = fallback(ctx, pid, uid)
	if err != nil {
		return "", fmt.Errorf("read process executable as target uid: %w", err)
	}
	return validateProcessExecutable(executable)
}

func validateProcessExecutable(executable string) (string, error) {
	if executable == "" {
		return "", errors.New("process executable is empty")
	}
	if len(executable) > 4096 || strings.ContainsRune(executable, '\x00') {
		return "", errors.New("process executable is invalid")
	}
	return executable, nil
}

func (p *LinuxPlatform) Processes(ctx context.Context, query ProcessQuery) ([]ProcessInfo, error) {
	limit := query.Limit
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	entries, err := os.ReadDir(p.procRoot)
	if err != nil {
		return nil, fmt.Errorf("read proc root: %w", err)
	}
	needle := strings.ToLower(strings.TrimSpace(query.Search))
	out := make([]ProcessInfo, 0, limit)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		info, err := p.Process(ctx, pid)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
				continue
			}
			continue // processes legitimately disappear during a /proc scan
		}
		if needle != "" && !strings.Contains(strings.ToLower(info.Name+" "+info.Command+" "+info.Executable), needle) {
			continue
		}
		out = append(out, info)
	}
	switch query.SortBy {
	case "memory":
		sort.Slice(out, func(i, j int) bool { return out[i].RSSBytes > out[j].RSSBytes })
	case "pid":
		sort.Slice(out, func(i, j int) bool { return out[i].PID < out[j].PID })
	default:
		sort.Slice(out, func(i, j int) bool { return out[i].CPUTicks > out[j].CPUTicks })
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (p *LinuxPlatform) Process(ctx context.Context, pid int) (ProcessInfo, error) {
	if pid <= 0 {
		return ProcessInfo{}, errors.New("pid must be positive")
	}
	base := filepath.Join(p.procRoot, strconv.Itoa(pid))
	statBytes, err := p.readContext(ctx, filepath.Join(base, "stat"))
	if err != nil {
		return ProcessInfo{}, err
	}
	info, err := parseProcessStat(strings.TrimSpace(string(statBytes)), uint64(os.Getpagesize()))
	if err != nil {
		return ProcessInfo{}, fmt.Errorf("parse process %d stat: %w", pid, err)
	}
	info.PID = pid
	if status, readErr := p.readContext(ctx, filepath.Join(base, "status")); readErr == nil {
		info.UID = parseProcessUID(status)
	}
	if command, readErr := p.readContext(ctx, filepath.Join(base, "cmdline")); readErr == nil {
		info.Command = redactCommand(command)
	}
	if executable, readErr := os.Readlink(filepath.Join(base, "exe")); readErr == nil {
		info.Executable = executable
	}
	return info, nil
}

func parseProcessStat(line string, pageSize uint64) (ProcessInfo, error) {
	open := strings.IndexByte(line, '(')
	close := strings.LastIndexByte(line, ')')
	if open < 1 || close <= open {
		return ProcessInfo{}, errors.New("invalid comm field")
	}
	name := line[open+1 : close]
	fields := strings.Fields(strings.TrimSpace(line[close+1:]))
	if len(fields) < 22 {
		return ProcessInfo{}, fmt.Errorf("got %d fields after comm", len(fields))
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return ProcessInfo{}, err
	}
	utime, err := strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return ProcessInfo{}, err
	}
	stime, err := strconv.ParseUint(fields[12], 10, 64)
	if err != nil {
		return ProcessInfo{}, err
	}
	start, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return ProcessInfo{}, err
	}
	rssPages, err := strconv.ParseInt(fields[21], 10, 64)
	if err != nil {
		return ProcessInfo{}, err
	}
	if rssPages < 0 {
		rssPages = 0
	}
	return ProcessInfo{PPID: ppid, Name: name, State: fields[0], StartTicks: start, CPUTicks: utime + stime, RSSBytes: uint64(rssPages) * pageSize}, nil
}

func parseProcessUID(status []byte) int {
	s := bufio.NewScanner(strings.NewReader(string(status)))
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) >= 2 && fields[0] == "Uid:" {
			value, err := strconv.Atoi(fields[1])
			if err == nil {
				return value
			}
		}
	}
	return -1
}

func redactCommand(raw []byte) string {
	command := strings.TrimSpace(strings.ReplaceAll(string(raw), "\x00", " "))
	if len(command) > 512 {
		command = command[:512] + "…"
	}
	parts := strings.Fields(command)
	for i, part := range parts {
		lower := strings.ToLower(part)
		for _, key := range []string{"password=", "passwd=", "token=", "secret=", "api_key=", "apikey="} {
			if index := strings.Index(lower, key); index >= 0 {
				parts[i] = part[:index+len(key)] + "[REDACTED]"
				break
			}
		}
	}
	return strings.Join(parts, " ")
}

func (p *LinuxPlatform) ProcessesByPort(ctx context.Context, port int) ([]ProcessInfo, error) {
	if port < 1 || port > 65535 {
		return nil, errors.New("port must be between 1 and 65535")
	}
	inodes, err := p.socketInodesForPort(ctx, port)
	if err != nil {
		return nil, err
	}
	if len(inodes) == 0 {
		return []ProcessInfo{}, nil
	}
	entries, err := os.ReadDir(p.procRoot)
	if err != nil {
		return nil, err
	}
	seen := map[int]bool{}
	var out []ProcessInfo
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || !entry.IsDir() {
			continue
		}
		fds, err := os.ReadDir(filepath.Join(p.procRoot, entry.Name(), "fd"))
		if err != nil {
			continue
		}
		matched := false
		for _, fd := range fds {
			target, err := os.Readlink(filepath.Join(p.procRoot, entry.Name(), "fd", fd.Name()))
			if err != nil {
				continue
			}
			if inode, ok := socketInode(target); ok && inodes[inode] {
				matched = true
				break
			}
		}
		if matched && !seen[pid] {
			info, err := p.Process(ctx, pid)
			if err == nil {
				out = append(out, info)
				seen[pid] = true
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PID < out[j].PID })
	return out, nil
}

func (p *LinuxPlatform) socketInodesForPort(ctx context.Context, port int) (map[string]bool, error) {
	out := map[string]bool{}
	for _, name := range []string{"tcp", "tcp6", "udp", "udp6"} {
		b, err := p.readContext(ctx, filepath.Join(p.procRoot, "net", name))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		scanner := bufio.NewScanner(strings.NewReader(string(b)))
		if scanner.Scan() {
		}
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) < 10 {
				continue
			}
			_, portHex, ok := strings.Cut(fields[1], ":")
			if !ok {
				continue
			}
			value, err := strconv.ParseInt(portHex, 16, 32)
			if err == nil && int(value) == port {
				out[fields[9]] = true
			}
		}
		if err := scanner.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}
func socketInode(target string) (string, bool) {
	if !strings.HasPrefix(target, "socket:[") || !strings.HasSuffix(target, "]") {
		return "", false
	}
	return strings.TrimSuffix(strings.TrimPrefix(target, "socket:["), "]"), true
}
