package gateway

import (
	"context"

	"go.uber.org/zap"

	"github.com/tsamsiyu/pontifex/apps/agent/internal/bgp"
	"github.com/tsamsiyu/pontifex/apps/agent/internal/cluster"
	"github.com/tsamsiyu/pontifex/apps/agent/internal/config"
	bgplib "github.com/tsamsiyu/pontifex/apps/agent/internal/libs/bgp"
	routeslib "github.com/tsamsiyu/pontifex/apps/agent/internal/libs/routes"
	bgprecon "github.com/tsamsiyu/pontifex/apps/agent/internal/reconcilers/bgp"
	routesrecon "github.com/tsamsiyu/pontifex/apps/agent/internal/reconcilers/routes"
)

// Manager hosts all gateway-mode reconcilers: BGP (route reflector + eBGP),
// routes (global RIB from BGP-learned prefixes), and WireGuard.
type Manager struct {
	cfg       config.GatewayConfig
	bgpServer bgplib.Server
	logger    *zap.Logger
}

// NewManager constructs a Manager. bgpServer must already be started by the
// caller (cmd/agent/main.go) before Run is invoked.
func NewManager(cfg config.GatewayConfig, bgpServer bgplib.Server, logger *zap.Logger) *Manager {
	return &Manager{cfg: cfg, bgpServer: bgpServer, logger: logger}
}

// Run wires all gateway components and blocks until ctx is cancelled.
func (m *Manager) Run(ctx context.Context) error {
	m.logger.Info("gateway manager started",
		zap.String("nodeName", m.cfg.NodeName),
		zap.Bool("isSecondary", m.cfg.IsSecondary),
		zap.Int("wgListenPort", m.cfg.WGListenPort),
	)

	obs := cluster.NewObserver(m.logger)
	med := cluster.NewMediator(obs, m.logger)

	bgpReconciler := bgprecon.NewGatewayReconciler(
		m.bgpServer, m.cfg.ASN, m.cfg.RouterID, m.cfg.IsSecondary, m.logger,
	)
	routesReconciler := routesrecon.NewGatewayReconciler(
		routeslib.NewNetlinkRoutes(), m.logger,
	)
	updater := bgp.NewRoutesUpdater(
		m.bgpServer.Subscribe(), med.Subscribe(), routesReconciler, m.logger,
	)
	bgpSnapshots := med.Subscribe()

	go func() { _ = med.Run(ctx) }()

	go func() {
		for {
			select {
			case snap := <-bgpSnapshots:
				if err := bgpReconciler.Reconcile(ctx, snap); err != nil {
					m.logger.Error("bgp reconcile", zap.Error(err))
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() { _ = updater.Run(ctx) }()

	<-ctx.Done()
	m.logger.Info("gateway manager stopped")
	return nil
}
