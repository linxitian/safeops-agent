package platform

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSocketsParsesProcNet(t *testing.T) {
	root := t.TempDir()
	proc := filepath.Join(root, "proc")
	etc := filepath.Join(root, "etc")
	mustWrite(t, filepath.Join(proc, "net", "tcp"), "sl local_address rem_address st tx_queue rx_queue tr tm->when retrnsmt uid timeout inode\n0: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000 1000 0 111 1\n1: 0200007F:01BB 0300007F:C350 01 00000000:00000000 00:00000000 00000000 1000 0 222 1\n")
	for _, name := range []string{"tcp6", "udp", "udp6"} {
		mustWrite(t, filepath.Join(proc, "net", name), "sl local_address rem_address st tx_queue rx_queue tr tm->when retrnsmt uid timeout inode\n")
	}
	p := NewLinux(WithRoots(proc, etc))
	listeners, err := p.Sockets(context.Background(), true, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(listeners) != 1 || listeners[0].LocalAddress != "127.0.0.1" || listeners[0].LocalPort != 8080 || listeners[0].State != "LISTEN" {
		t.Fatalf("unexpected listeners: %+v", listeners)
	}
	connections, err := p.Sockets(context.Background(), false, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(connections) != 2 || connections[0].State != "ESTABLISHED" || connections[0].LocalPort != 443 {
		t.Fatalf("unexpected connections: %+v", connections)
	}
}

func TestInterfacesReadsRealHost(t *testing.T) {
	p := NewLinux()
	interfaces, err := p.Interfaces(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(interfaces) == 0 {
		t.Fatal("no network interfaces found")
	}
}
