package gateway

import (
	"context"

	"go.uber.org/zap"

	"github.com/tsamsiyu/pontifex/apps/agent/internal/config"
)

// Manager hosts the gateway-mode controller-runtime manager and all gateway
// reconcilers (WireGuard, BGP, routes). Phase 1 is a stub: it only blocks
// until ctx is done.
type Manager struct {
	cfg    config.GatewayConfig
	logger *zap.Logger
}

// NewManager constructs a Manager.
func NewManager(cfg config.GatewayConfig, logger *zap.Logger) *Manager {
	return &Manager{cfg: cfg, logger: logger}
}

// Run starts the manager and blocks until ctx is cancelled.
func (m *Manager) Run(ctx context.Context) error {
	m.logger.Info("gateway manager started",
		zap.String("nodeName", m.cfg.NodeName),
		zap.Bool("isSecondary", m.cfg.IsSecondary),
		zap.Int("wgListenPort", m.cfg.WGListenPort),
	)
	<-ctx.Done()
	m.logger.Info("manager stopped")
	return nil
}
