package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"safeops-agent/internal/approval"
	"safeops-agent/internal/executor"
	"safeops-agent/internal/guard"
	"safeops-agent/internal/platform"
	"safeops-agent/internal/rollback"
	"syscall"
	"time"
)

func main() {
	socketPath := flag.String("socket", "/run/safeops/privexec.sock", "Unix domain socket path")
	dataDir := flag.String("data", "/var/lib/safeops", "persistent state root")
	policyPath := flag.String("policy", "/etc/safeops/policies/tools.yaml", "local Tool policy")
	configPath := flag.String("config", "/etc/safeops/executor.yaml", "executor allowlist config")
	secretPath := flag.String("secret", "/etc/safeops/privexec.hmac", "shared HMAC secret file")
	mode := flag.String("mode", "dry-run", "execution mode: dry-run or lab")
	flag.Parse()
	if *mode != "dry-run" && *mode != "lab" {
		log.Fatal("execution mode must be dry-run or lab")
	}
	secret, err := executor.ReadSecretFile(*secretPath)
	if err != nil {
		log.Fatal(err)
	}
	catalog, err := guard.LoadCatalog(*policyPath)
	if err != nil {
		log.Fatal(err)
	}
	config, err := executor.LoadConfig(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	approvals, err := approval.NewStore(filepath.Join(*dataDir, "approvals"))
	if err != nil {
		log.Fatal(err)
	}
	nonces, err := executor.NewNonceStore(filepath.Join(*dataDir, "state", "used-nonces.json"))
	if err != nil {
		log.Fatal(err)
	}
	targets := executor.LinuxTargets{Linux: platform.NewLinux(), Commands: platform.NewCommandPlatform(), AllowedFileRoots: config.AllowedFileRoots}
	validator := executor.Validator{Secret: secret, Pipeline: guard.NewSafetyPipeline(catalog), Nonces: nonces, Approvals: approvals, Scope: config.Scope(), Targets: targets}
	dry := executor.DryRunHandler{}
	handlers := map[string]executor.Handler{"service.restart": dry, "service.start": dry, "service.stop": dry, "process.terminate": dry, "file.quarantine": dry, "file.restore_quarantine": dry, "file.create": dry, "file.delete": dry}
	executionMode := executor.DryRun
	if *mode == "lab" {
		manager, err := rollback.NewQuarantineManager(config.LabFileRoots, config.QuarantineRoot)
		if err != nil {
			log.Fatal(err)
		}
		if err := manager.Recover(context.Background()); err != nil {
			log.Fatal(err)
		}
		quarantine := executor.QuarantineHandler{Manager: manager}
		handlers = map[string]executor.Handler{
			"file.quarantine":         quarantine,
			"file.restore_quarantine": quarantine,
			"file.delete":             quarantine,
			"file.create":             executor.FileCreateHandler{},
			"service.restart":         executor.ServiceRestartHandler{Commands: targets.Commands},
			"process.terminate":       executor.ProcessTerminateHandler{Linux: targets.Linux},
		}
		executionMode = executor.LabSandbox
	}
	engine := executor.Executor{Validator: validator, Handlers: handlers}
	listener, err := listenUnix(*socketPath)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = listener.Close(); _ = os.Remove(*socketPath) }()
	server := &http.Server{Handler: executor.HTTPHandler{Executor: engine, Mode: executionMode}.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	log.Printf("safeops-privexec %s listening on unix://%s", executionMode, *socketPath)
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
func listenUnix(path string) (net.Listener, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("executor socket path must be absolute")
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, errors.New("refusing to replace non-socket path")
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o660); err != nil {
		listener.Close()
		return nil, err
	}
	return listener, nil
}
