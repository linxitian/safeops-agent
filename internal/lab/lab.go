package lab

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	MaxLogBytes    int64 = 64 << 20
	MaxLogRate     int64 = 1 << 20
	MaxLabDuration       = 10 * time.Minute
)

func ValidateLoopbackAddress(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("Lab listener must use an explicit loopback IP")
	}
	value, err := net.LookupPort("tcp", port)
	if err != nil {
		return err
	}
	if value < 1024 || value > 65535 {
		return errors.New("Lab listener port must be between 1024 and 65535")
	}
	return nil
}

type CPUConfig struct {
	DutyPercent int
	Workers     int
	Duration    time.Duration
}

func ValidateCPUConfig(config CPUConfig) error {
	if config.DutyPercent < 10 || config.DutyPercent > 90 {
		return errors.New("CPU duty must be between 10 and 90 percent")
	}
	if config.Workers < 1 || config.Workers > 4 || config.Workers > runtime.NumCPU() {
		return errors.New("CPU workers must be between 1 and min(4, host CPUs)")
	}
	if config.Duration <= 0 || config.Duration > MaxLabDuration {
		return errors.New("CPU Lab duration must be positive and at most 10 minutes")
	}
	return nil
}

func RunCPU(ctx context.Context, config CPUConfig) error {
	if err := ValidateCPUConfig(config); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, config.Duration)
	defer cancel()
	var wait sync.WaitGroup
	for worker := 0; worker < config.Workers; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			const cycle = 100 * time.Millisecond
			busy := time.Duration(config.DutyPercent) * cycle / 100
			idle := cycle - busy
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				deadline := time.Now().Add(busy)
				for time.Now().Before(deadline) {
					_ = time.Now().UnixNano() * 31
				}
				timer := time.NewTimer(idle)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
		}()
	}
	wait.Wait()
	return nil
}

type LogConfig struct {
	Root      string
	Path      string
	MaxBytes  int64
	RateBytes int64
	Duration  time.Duration
}

type LogResult struct {
	Path         string `json:"path"`
	InitialBytes int64  `json:"initial_bytes"`
	FinalBytes   int64  `json:"final_bytes"`
	WrittenBytes int64  `json:"written_bytes"`
}

func WriteBoundedLog(ctx context.Context, config LogConfig) (LogResult, error) {
	path, err := validateLabFile(config.Root, config.Path)
	if err != nil {
		return LogResult{}, err
	}
	if config.MaxBytes <= 0 || config.MaxBytes > MaxLogBytes {
		return LogResult{}, fmt.Errorf("max bytes must be between 1 and %d", MaxLogBytes)
	}
	if config.RateBytes <= 0 || config.RateBytes > MaxLogRate {
		return LogResult{}, fmt.Errorf("rate must be between 1 and %d bytes/second", MaxLogRate)
	}
	if config.Duration <= 0 || config.Duration > MaxLabDuration {
		return LogResult{}, errors.New("log Lab duration must be positive and at most 10 minutes")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return LogResult{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return LogResult{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > config.MaxBytes {
		return LogResult{}, errors.New("Lab log is not regular or already exceeds max bytes")
	}
	result := LogResult{Path: path, InitialBytes: info.Size(), FinalBytes: info.Size()}
	ctx, cancel := context.WithTimeout(ctx, config.Duration)
	defer cancel()
	chunkSize := int64(4096)
	if config.RateBytes < chunkSize {
		chunkSize = config.RateBytes
	}
	chunk := []byte(strings.Repeat("safeops-lab bounded log record\n", int(chunkSize/31)+1))[:chunkSize]
	interval := time.Duration(float64(time.Second) * float64(chunkSize) / float64(config.RateBytes))
	if interval < time.Millisecond {
		interval = time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for result.FinalBytes < config.MaxBytes {
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return result, nil
			}
			return result, ctx.Err()
		case <-ticker.C:
			remaining := config.MaxBytes - result.FinalBytes
			toWrite := chunk
			if int64(len(toWrite)) > remaining {
				toWrite = toWrite[:remaining]
			}
			n, err := file.Write(toWrite)
			result.WrittenBytes += int64(n)
			result.FinalBytes += int64(n)
			if err != nil {
				return result, err
			}
		}
	}
	if err := file.Sync(); err != nil {
		return result, err
	}
	return result, nil
}

func validateLabFile(root, path string) (string, error) {
	if !filepath.IsAbs(root) || !filepath.IsAbs(path) {
		return "", errors.New("Lab root and file path must be absolute")
	}
	canonicalRoot, err := filepath.EvalSymlinks(filepath.Clean(root))
	if err != nil {
		return "", err
	}
	canonicalParent, err := filepath.EvalSymlinks(filepath.Dir(filepath.Clean(path)))
	if err != nil {
		return "", err
	}
	resolved := filepath.Join(canonicalParent, filepath.Base(path))
	relative, err := filepath.Rel(canonicalRoot, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("Lab log path escapes configured root")
	}
	if info, err := os.Lstat(resolved); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("Lab log path must not be a symlink")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return resolved, nil
}
