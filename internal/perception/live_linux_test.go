//go:build linux

package perception

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"safeops-agent/internal/platform"
	"safeops-agent/internal/safefs"
)

func TestLiveReadOnlyLinuxCollectors(t *testing.T) {
	managed := t.TempDir()
	if err := os.WriteFile(filepath.Join(managed, "smoke.conf"), []byte("enabled=true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reader, err := safefs.NewReader(managed)
	if err != nil {
		t.Fatal(err)
	}
	linux := platform.NewLinux()
	batch := CollectAll(context.Background(), []Collector{
		ProcfsCollector{Platform: linux, Processes: linux, ProcessLimit: 10},
		DiskCollector{Platform: linux, Reader: reader, Paths: []string{managed}, MaxMounts: 128, MaxEntries: 100},
		NetworkCollector{Platform: linux, SocketLimit: 100},
		SystemConfigCollector{Platform: linux, Sysctls: linux, SelectedSysctls: []string{"kernel.pid_max"}, MaxMounts: 128},
		ConfigChangeCollector{Reader: reader, Limit: 20},
	}, CollectionLimits{PerCollectorTimeout: 5 * time.Second, MaxObservations: 2000})

	sources := map[string]bool{}
	for _, value := range batch.Observations {
		sources[value.Source] = true
	}
	for _, source := range []string{"procfs", "disk", "network", "system_config", "config_change"} {
		if !sources[source] {
			t.Errorf("live collector %s produced no valid observation; issues=%+v", source, batch.Issues)
		}
	}
	if batch.Truncated {
		t.Fatalf("live batch exceeded its explicit observation budget: %d", len(batch.Observations))
	}
}
