package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"safeops-agent/internal/agent"
	"safeops-agent/internal/api"
	"safeops-agent/internal/approval"
	"safeops-agent/internal/executor"
	"safeops-agent/internal/guard"
	"safeops-agent/internal/llm"
	"safeops-agent/internal/platform"
	"safeops-agent/internal/registry"
	"safeops-agent/internal/storage"
	"safeops-agent/internal/trace"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8080", "HTTP listen address")
	dataDir := flag.String("data", "./data", "persistent data directory")
	registryConfig := flag.String("mcp-config", "./config/mcp_servers.yaml", "MCP server manifest file")
	policyConfig := flag.String("policy", "./policies/tools.yaml", "local Tool security policy file")
	executorSocket := flag.String("executor-socket", "/run/safeops/privexec.sock", "privileged executor Unix socket")
	executorConfig := flag.String("executor-config", "./config/executor.yaml", "executor allowlist config used to snapshot action targets")
	executorSecret := flag.String("executor-secret", "", "shared HMAC secret file; empty disables write-action preparation")
	webRoot := flag.String("web-root", "", "prebuilt frontend directory; empty disables static frontend serving")
	maxConcurrentTasks := flag.Int("max-concurrent-tasks", 8, "maximum concurrent Agent runs and approval resumes")
	maxSessions := flag.Int("max-sessions", 1000, "maximum retained Sessions")
	maxTasks := flag.Int("max-tasks", 10000, "maximum retained Tasks")
	flag.Parse()
	if err := validateListenAddress(*listen); err != nil {
		log.Fatal(err)
	}
	if *maxConcurrentTasks <= 0 || *maxSessions <= 0 || *maxTasks <= 0 {
		log.Fatal("runtime limits must be positive")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	store, err := storage.NewFileStore(*dataDir)
	if err != nil {
		log.Fatal(err)
	}
	traceWriter, err := trace.NewWriter(*dataDir + "/traces")
	if err != nil {
		log.Fatal(err)
	}
	runtimeLog, err := openRuntimeLog(*dataDir)
	if err != nil {
		log.Fatal(err)
	}
	defer runtimeLog.Close()
	cfg, err := registry.Load(*registryConfig)
	if err != nil {
		log.Fatal(err)
	}
	reg := registry.New(cfg)
	defer reg.Close()
	startupCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	err = reg.Start(startupCtx)
	cancel()
	if err != nil {
		log.Fatal(err)
	}
	catalog, err := guard.LoadCatalog(*policyConfig)
	if err != nil {
		log.Fatal(err)
	}
	safety := guard.NewSafetyPipeline(catalog)
	execConfig, err := executor.LoadConfig(*executorConfig)
	if err != nil {
		log.Fatal(err)
	}
	targets := executor.NewMutableTargets(platform.NewLinux(), platform.NewCommandPlatform(), execConfig.AllowedFileRoots)
	allowlistManager, err := executor.NewConfigManager(filepath.Join(*dataDir, "state", "executor_allowlist.yaml"), execConfig, targets)
	if err != nil {
		log.Fatal(err)
	}
	planner := llm.NewRuntimeProvider()
	llmSettings := llm.NewSettingsStore(filepath.Join(*dataDir, "state", "llm_config.json"))
	if stored, loadErr := llmSettings.Load(); loadErr == nil {
		if err := planner.Configure(stored.Config(), "web", stored.UpdatedAt); err != nil {
			log.Fatal(err)
		}
		log.Printf("OpenAI-compatible provider enabled from Web configuration for model %s", stored.Model)
	} else if !errors.Is(loadErr, llm.ErrNotConfigured) {
		log.Fatal(loadErr)
	} else {
		llmConfig, err := llm.ConfigFromEnv()
		switch {
		case errors.Is(err, llm.ErrNotConfigured):
			log.Print("OpenAI-compatible provider is not configured; deterministic CPU/memory slice remains available")
		case err != nil:
			log.Fatal(err)
		default:
			if err := planner.Configure(llmConfig, "env", time.Now().UTC()); err != nil {
				log.Fatal(err)
			}
			log.Printf("OpenAI-compatible provider enabled from environment for model %s", llmConfig.Model)
		}
	}
	approvalStore, err := approval.NewStore(*dataDir + "/approvals")
	if err != nil {
		log.Fatal(err)
	}
	orchestrator := &agent.Orchestrator{Store: store, Registry: reg, Capabilities: reg, Planner: planner, Safety: safety, Trace: traceWriter, ToolTimeout: 10 * time.Second, WorkerID: "server-" + time.Now().UTC().Format("20060102T150405.000000000")}
	if *executorSecret == "" {
		log.Print("write-action preparation is disabled; set -executor-secret to enable approval-bound actions")
	} else {
		secret, err := executor.ReadSecretFile(*executorSecret)
		if err != nil {
			log.Fatal(err)
		}
		orchestrator.Actions = &agent.ActionPreparer{Store: store, Approvals: approvalStore, Safety: safety, Scope: execConfig.Scope(), Trace: traceWriter, Secret: secret}
		orchestrator.FileTargets = targets
		orchestrator.ActionTargets = targets
		log.Print("approval-bound write-action preparation enabled")
	}
	executorClient, err := executor.NewUnixClient(*executorSocket)
	if err != nil {
		log.Fatal(err)
	}
	resumer := agent.ApprovalResumer{Store: store, Executor: executorClient, Trace: traceWriter, Continuation: orchestrator}
	apiOptions := []api.Option{api.WithApprovals(approvalStore, resumer), api.WithLLM(planner, llmSettings), api.WithRuntimeLog(runtimeLog), api.WithExecutorAllowlist(allowlistManager), api.WithLimits(api.Limits{MaxConcurrentTasks: *maxConcurrentTasks, MaxSessions: *maxSessions, MaxTasks: *maxTasks})}
	if *webRoot != "" {
		index := filepath.Join(*webRoot, "index.html")
		info, err := os.Stat(index)
		if err != nil || !info.Mode().IsRegular() {
			log.Fatalf("web root must contain a regular index.html: %s", index)
		}
		apiOptions = append(apiOptions, api.WithWebRoot(*webRoot))
		log.Printf("serving prebuilt frontend from %s", *webRoot)
	}
	apiServer := api.New(store, reg, orchestrator, traceWriter, apiOptions...)
	resumeResolvedApprovals(ctx, approvalStore, resumer)
	go func() {
		for _, recoveryErr := range orchestrator.RecoverIncomplete(ctx, nil) {
			log.Printf("unfinished task recovery: %v", recoveryErr)
		}
	}()
	server := &http.Server{Addr: *listen, Handler: apiServer.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 0, IdleTimeout: 60 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	log.Printf("SafeOps server listening on http://%s", *listen)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func validateListenAddress(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid HTTP listen address: %w", err)
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("unauthenticated SafeOps API must listen on a loopback address")
	}
	return nil
}

func openRuntimeLog(dataDir string) (*os.File, error) {
	dir := filepath.Join(dataDir, "logs")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	return os.OpenFile(filepath.Join(dir, "runtime.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}

func resumeResolvedApprovals(ctx context.Context, store *approval.Store, resumer agent.ApprovalResumer) {
	records, err := store.List(ctx)
	if err != nil {
		log.Printf("scan approvals for resume: %v", err)
		return
	}
	for _, record := range records {
		if record.Status != approval.Approved && record.Status != approval.Rejected && record.Status != approval.Expired {
			continue
		}
		resumeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		_, err := resumer.Resume(resumeCtx, record)
		cancel()
		if err != nil && !errors.Is(err, storage.ErrNotFound) {
			log.Printf("resume approval %s: %v", record.ID, err)
		}
	}
}
