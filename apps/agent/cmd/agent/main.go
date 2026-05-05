package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/tsamsiyu/pontifex/apps/agent/internal/config"
	"github.com/tsamsiyu/pontifex/apps/agent/internal/controllers/gateway"
	"github.com/tsamsiyu/pontifex/apps/agent/internal/controllers/internalnode"
	bgplib "github.com/tsamsiyu/pontifex/apps/agent/internal/libs/bgp"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	mode := flag.String("mode", "", "agent mode: gateway | internal")
	configPath := flag.String("config", "/etc/pontifex/agent.yaml", "path to YAML config file")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	switch *mode {
	case "gateway":
		cfg, err := config.LoadGateway(*configPath)
		if err != nil {
			return err
		}
		logger, err := newLogger(cfg.LogLevel)
		if err != nil {
			return err
		}
		defer func() { _ = logger.Sync() }()
		bgpServer := bgplib.NewGoBGPServer()
		if err := bgpServer.Start(ctx, cfg.ASN, cfg.RouterID); err != nil {
			return fmt.Errorf("start bgp server: %w", err)
		}
		return gateway.NewManager(cfg, bgpServer, logger).Run(ctx)

	case "internal":
		cfg, err := config.LoadInternalNode(*configPath)
		if err != nil {
			return err
		}
		logger, err := newLogger(cfg.LogLevel)
		if err != nil {
			return err
		}
		defer func() { _ = logger.Sync() }()
		bgpServer := bgplib.NewGoBGPServer()
		if err := bgpServer.Start(ctx, cfg.ASN, cfg.RouterID); err != nil {
			return fmt.Errorf("start bgp server: %w", err)
		}
		return internalnode.NewManager(cfg, bgpServer, logger).Run(ctx)

	case "":
		return fmt.Errorf("--mode is required (gateway | internal)")
	default:
		return fmt.Errorf("unknown --mode %q", *mode)
	}
}

func newLogger(level string) (*zap.Logger, error) {
	cfg := zap.NewProductionConfig()
	if level != "" {
		var lvl zapcore.Level
		if err := lvl.UnmarshalText([]byte(level)); err != nil {
			return nil, fmt.Errorf("parse log level %q: %w", level, err)
		}
		cfg.Level = zap.NewAtomicLevelAt(lvl)
	}
	return cfg.Build()
}
