package lab

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLabNetworkAndCPULimits(t *testing.T) {
	for _, allowed := range []string{"127.0.0.1:18081", "[::1]:18081"} {
		if err := ValidateLoopbackAddress(allowed); err != nil {
			t.Fatalf("%s rejected: %v", allowed, err)
		}
	}
	for _, denied := range []string{"0.0.0.0:18081", "127.0.0.1:80", "example.com:18081"} {
		if err := ValidateLoopbackAddress(denied); err == nil {
			t.Fatalf("%s accepted", denied)
		}
	}
	if err := ValidateCPUConfig(CPUConfig{DutyPercent: 91, Workers: 1, Duration: time.Second}); err == nil {
		t.Fatal("unsafe CPU duty accepted")
	}
}

func TestBoundedLogNeverExceedsLimitOrEscapesRoot(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "growth.log")
	result, err := WriteBoundedLog(context.Background(), LogConfig{Root: root, Path: path, MaxBytes: 8192, RateBytes: MaxLogRate, Duration: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalBytes != 8192 || result.WrittenBytes != 8192 {
		t.Fatalf("unexpected bounded result: %+v", result)
	}
	if info, err := os.Stat(path); err != nil || info.Size() > 8192 {
		t.Fatalf("file exceeded limit: %+v %v", info, err)
	}
	if _, err := WriteBoundedLog(context.Background(), LogConfig{Root: root, Path: filepath.Join(root, "..", "escape.log"), MaxBytes: 1, RateBytes: 1, Duration: time.Second}); err == nil {
		t.Fatal("path escape accepted")
	}
}
