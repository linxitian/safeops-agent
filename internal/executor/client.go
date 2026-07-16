package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"time"

	"safeops-agent/contracts"
)

type UnixClient struct {
	socket string
	client *http.Client
}

func NewUnixClient(socket string) (*UnixClient, error) {
	if !filepath.IsAbs(socket) {
		return nil, errors.New("executor socket path must be absolute")
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return dialer.DialContext(ctx, "unix", socket)
	}}
	return &UnixClient{socket: socket, client: &http.Client{Transport: transport, Timeout: 20 * time.Second}}, nil
}

func (c *UnixClient) Execute(ctx context.Context, envelope contracts.ActionEnvelope) (Result, error) {
	b, err := json.Marshal(envelope)
	if err != nil {
		return Result{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://safeops.local/v1/execute", bytes.NewReader(b))
	if err != nil {
		return Result{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.client.Do(request)
	if err != nil {
		return Result{}, fmt.Errorf("call privileged executor on unix socket: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20+1))
	if err != nil {
		return Result{}, err
	}
	if len(body) > 1<<20 {
		return Result{}, errors.New("executor response exceeds 1 MiB")
	}
	if response.StatusCode != http.StatusOK {
		var failure struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &failure) == nil && failure.Error != "" {
			return Result{}, fmt.Errorf("executor denied envelope: %s", failure.Error)
		}
		return Result{}, fmt.Errorf("executor returned HTTP %d", response.StatusCode)
	}
	var result Result
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return Result{}, err
	}
	return result, nil
}
