package internalnode

import (
	"context"

	"go.uber.org/zap"

	"github.com/tsamsiyu/pontifex/apps/agent/internal/config"
)

// Manager hosts the internal-node controller-runtime manager and all
// internal-node reconcilers (BGP, routes). Phase 1 is a stub: it only blocks
// until ctx is done.
type Manager struct {
	cfg    config.InternalNodeConfig
	logger *zap.Logger
}

// NewManager constructs a Manager.
func NewManager(cfg config.InternalNodeConfig, logger *zap.Logger) *Manager {
	return &Manager{cfg: cfg, logger: logger}
}

// Run starts the manager and blocks until ctx is cancelled.
func (m *Manager) Run(ctx context.Context) error {
	m.logger.Info("internal-node manager started",
		zap.String("nodeName", m.cfg.NodeName),
		zap.String("firewall", m.cfg.Firewall),
	)
	<-ctx.Done()
	m.logger.Info("manager stopped")
	return nil
}
