package platform

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type SocketInfo struct {
	Protocol      string `json:"protocol"`
	LocalAddress  string `json:"local_address"`
	LocalPort     int    `json:"local_port"`
	RemoteAddress string `json:"remote_address"`
	RemotePort    int    `json:"remote_port"`
	State         string `json:"state"`
	Inode         string `json:"inode"`
	Listening     bool   `json:"listening"`
}

type InterfaceInfo struct {
	Name            string   `json:"name"`
	Index           int      `json:"index"`
	MTU             int      `json:"mtu"`
	HardwareAddress string   `json:"hardware_address,omitempty"`
	Flags           []string `json:"flags"`
	Addresses       []string `json:"addresses"`
	RXBytes         uint64   `json:"rx_bytes"`
	TXBytes         uint64   `json:"tx_bytes"`
}

func (p *LinuxPlatform) Sockets(ctx context.Context, listeningOnly bool, limit int) ([]SocketInfo, error) {
	if limit <= 0 || limit > 5000 {
		limit = 500
	}
	var out []SocketInfo
	for _, table := range []struct {
		name, protocol string
		ipv6           bool
	}{{"tcp", "tcp", false}, {"tcp6", "tcp6", true}, {"udp", "udp", false}, {"udp6", "udp6", true}} {
		b, err := p.readContext(ctx, filepath.Join(p.procRoot, "net", table.name))
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
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			fields := strings.Fields(scanner.Text())
			if len(fields) < 10 {
				continue
			}
			localAddress, localPort, err := parseProcEndpoint(fields[1], table.ipv6)
			if err != nil {
				continue
			}
			remoteAddress, remotePort, err := parseProcEndpoint(fields[2], table.ipv6)
			if err != nil {
				continue
			}
			state := socketState(table.protocol, fields[3])
			listening := state == "LISTEN" || (strings.HasPrefix(table.protocol, "udp") && remotePort == 0)
			if listeningOnly && !listening {
				continue
			}
			out = append(out, SocketInfo{Protocol: table.protocol, LocalAddress: localAddress, LocalPort: localPort, RemoteAddress: remoteAddress, RemotePort: remotePort, State: state, Inode: fields[9], Listening: listening})
			if len(out) >= limit {
				return out, nil
			}
		}
		if err := scanner.Err(); err != nil {
			return nil, err
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].LocalPort == out[j].LocalPort {
			return out[i].Protocol < out[j].Protocol
		}
		return out[i].LocalPort < out[j].LocalPort
	})
	return out, nil
}

func parseProcEndpoint(value string, ipv6 bool) (string, int, error) {
	addressHex, portHex, ok := strings.Cut(value, ":")
	if !ok {
		return "", 0, errors.New("invalid endpoint")
	}
	port64, err := strconv.ParseUint(portHex, 16, 16)
	if err != nil {
		return "", 0, err
	}
	raw, err := hex.DecodeString(addressHex)
	if err != nil {
		return "", 0, err
	}
	if (!ipv6 && len(raw) != 4) || (ipv6 && len(raw) != 16) {
		return "", 0, errors.New("invalid address length")
	}
	if ipv6 {
		for i := 0; i < len(raw); i += 4 {
			raw[i], raw[i+3] = raw[i+3], raw[i]
			raw[i+1], raw[i+2] = raw[i+2], raw[i+1]
		}
	} else {
		raw[0], raw[3] = raw[3], raw[0]
		raw[1], raw[2] = raw[2], raw[1]
	}
	return net.IP(raw).String(), int(port64), nil
}
func socketState(protocol, code string) string {
	if strings.HasPrefix(protocol, "udp") {
		switch code {
		case "07":
			return "UNCONN"
		case "01":
			return "ESTABLISHED"
		}
	}
	states := map[string]string{"01": "ESTABLISHED", "02": "SYN_SENT", "03": "SYN_RECV", "04": "FIN_WAIT1", "05": "FIN_WAIT2", "06": "TIME_WAIT", "07": "CLOSE", "08": "CLOSE_WAIT", "09": "LAST_ACK", "0A": "LISTEN", "0B": "CLOSING"}
	if state := states[code]; state != "" {
		return state
	}
	return code
}

func (p *LinuxPlatform) Interfaces(ctx context.Context) ([]InterfaceInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	stats, err := p.interfaceStats(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]InterfaceInfo, 0, len(interfaces))
	for _, item := range interfaces {
		addresses, err := item.Addrs()
		if err != nil {
			continue
		}
		values := make([]string, 0, len(addresses))
		for _, address := range addresses {
			values = append(values, address.String())
		}
		stat := stats[item.Name]
		out = append(out, InterfaceInfo{Name: item.Name, Index: item.Index, MTU: item.MTU, HardwareAddress: item.HardwareAddr.String(), Flags: strings.Split(item.Flags.String(), "|"), Addresses: values, RXBytes: stat[0], TXBytes: stat[1]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return out, nil
}
func (p *LinuxPlatform) interfaceStats(ctx context.Context) (map[string][2]uint64, error) {
	b, err := p.readContext(ctx, filepath.Join(p.procRoot, "net", "dev"))
	if err != nil {
		return nil, err
	}
	out := map[string][2]uint64{}
	scanner := bufio.NewScanner(strings.NewReader(string(b)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		name, values, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		fields := strings.Fields(values)
		if len(fields) < 9 {
			continue
		}
		rx, e1 := strconv.ParseUint(fields[0], 10, 64)
		tx, e2 := strconv.ParseUint(fields[8], 10, 64)
		if e1 == nil && e2 == nil {
			out[strings.TrimSpace(name)] = [2]uint64{rx, tx}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("parse /proc/net/dev: %w", err)
	}
	return out, nil
}
