package platform

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestLiveLabPortCorrelation is opt-in because it requires a controlled Lab
// listener. target-test starts safeops-port-holder and sets SAFEOPS_LAB_PORT.
func TestLiveLabPortCorrelation(t *testing.T) {
	value := os.Getenv("SAFEOPS_LAB_PORT")
	if value == "" {
		t.Skip("SAFEOPS_LAB_PORT is not set")
	}
	port, err := strconv.Atoi(value)
	if err != nil {
		t.Fatal(err)
	}
	linux := NewLinux()
	sockets, err := linux.Sockets(context.Background(), true, 5000)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, socket := range sockets {
		if socket.LocalPort == port && socket.Listening {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("listener %d not found", port)
	}
	processes, err := linux.ProcessesByPort(context.Background(), port)
	if err != nil {
		t.Fatal(err)
	}
	for _, process := range processes {
		if strings.Contains(process.Executable, "safeops-port-holder") {
			return
		}
	}
	t.Fatalf("port-holder ownership not correlated: %+v", processes)
}
