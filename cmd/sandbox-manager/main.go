package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/actionlab-ai/aisphere-sandbox/internal/api"
	"github.com/actionlab-ai/aisphere-sandbox/internal/config"
	"github.com/actionlab-ai/aisphere-sandbox/internal/sandbox"
	"github.com/actionlab-ai/aisphere-sandbox/internal/store"
)

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", os.Getenv("SANDBOX_MANAGER_CONFIG"), "path to config YAML or JSON")
	flag.Parse()

	cfg, err := config.Load(configPath)
	if err != nil {
		slog.Error("load config failed", "error", err)
		os.Exit(1)
	}
	sandboxCfg := sandbox.Config{
		Enabled:                true,
		Driver:                 cfg.Sandbox.Driver,
		Namespace:              cfg.Sandbox.Namespace,
		CreateNamespace:        cfg.Sandbox.CreateNamespace,
		APIServer:              cfg.Sandbox.APIServer,
		Kubeconfig:             cfg.Sandbox.Kubeconfig,
		Token:                  cfg.Sandbox.Token,
		TokenFile:              cfg.Sandbox.TokenFile,
		CAFile:                 cfg.Sandbox.CAFile,
		Insecure:               cfg.Sandbox.Insecure,
		ServiceAccount:         cfg.Sandbox.ServiceAccount,
		RuntimeClassName:       cfg.Sandbox.RuntimeClassName,
		NetworkPolicyEnabled:   cfg.Sandbox.NetworkPolicyEnabled,
		DefaultNetworkMode:     cfg.Sandbox.DefaultNetworkMode,
		DefaultEgressCIDRs:     cfg.Sandbox.DefaultEgressCIDRs,
		Image:                  cfg.Sandbox.Image,
		ImagePullPolicy:        cfg.Sandbox.ImagePullPolicy,
		WorkspaceMountPath:     cfg.Sandbox.WorkspaceMountPath,
		StorageClass:           cfg.Sandbox.StorageClass,
		WorkspaceSize:          cfg.Sandbox.WorkspaceSize,
		ToolPort:               cfg.Sandbox.ToolPort,
		BrowserPort:            cfg.Sandbox.BrowserPort,
		VNCOrWebPort:           cfg.Sandbox.VNCOrWebPort,
		DefaultCPU:             cfg.Sandbox.DefaultCPU,
		DefaultMemory:          cfg.Sandbox.DefaultMemory,
		MaxCPU:                 cfg.Sandbox.MaxCPU,
		MaxMemory:              cfg.Sandbox.MaxMemory,
		IdleTTLSeconds:         cfg.Sandbox.IdleTTLSeconds,
		LeaseTTLSeconds:        cfg.Sandbox.LeaseTTLSeconds,
		AgentSandboxAPIVersion: cfg.Sandbox.AgentSandboxAPIVersion,
		UseClaim:               cfg.Sandbox.UseClaim,
		DefaultTemplate:        cfg.Sandbox.DefaultTemplate,
		DefaultWarmPool:        cfg.Sandbox.DefaultWarmPool,
		DefaultProfile:         cfg.Sandbox.DefaultProfile,
	}
	var mgr sandbox.Manager
	switch cfg.Sandbox.Driver {
	case "", "agent-sandbox", "agent-sandbox-crd":
		mgr, err = sandbox.NewAgentSandboxManager(sandboxCfg)
	case "direct-kubernetes", "kubernetes":
		mgr, err = sandbox.NewKubernetesManager(sandboxCfg)
	default:
		slog.Error("unsupported sandbox driver", "driver", cfg.Sandbox.Driver)
		os.Exit(1)
	}
	if err != nil {
		slog.Error("create sandbox manager failed", "error", err)
		os.Exit(1)
	}

	var leaseStore *store.PostgresStore
	if cfg.Database.Driver == "postgres" && cfg.Database.DSN != "" {
		leaseStore, err = store.NewPostgres(cfg.Database.DSN)
		if err != nil {
			slog.Error("create postgres lease store failed", "error", err)
			os.Exit(1)
		}
		defer leaseStore.Close()
	}

	srv := &http.Server{Addr: cfg.Server.Addr, Handler: api.NewWithStore(cfg, mgr, leaseStore).Handler(), ReadHeaderTimeout: 10 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	errCh := make(chan error, 1)
	go func() {
		slog.Info("aisphere-sandbox listening", "addr", cfg.Server.Addr)
		errCh <- srv.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server failed", "error", err)
			os.Exit(1)
		}
	}
}
