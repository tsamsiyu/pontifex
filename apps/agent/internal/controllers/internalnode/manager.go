package internalnode

import (
	"context"

	"go.uber.org/zap"

	"github.com/tsamsiyu/pontifex/apps/agent/internal/bgp"
	"github.com/tsamsiyu/pontifex/apps/agent/internal/cluster"
	"github.com/tsamsiyu/pontifex/apps/agent/internal/config"
	bgplib "github.com/tsamsiyu/pontifex/apps/agent/internal/libs/bgp"
	"github.com/tsamsiyu/pontifex/apps/agent/internal/libs/firewall"
	routeslib "github.com/tsamsiyu/pontifex/apps/agent/internal/libs/routes"
	bgprecon "github.com/tsamsiyu/pontifex/apps/agent/internal/reconcilers/bgp"
	routesrecon "github.com/tsamsiyu/pontifex/apps/agent/internal/reconcilers/routes"
)

// Manager hosts all internal-node reconcilers: BGP (iBGP to gateways),
// routes (per-overlay VRFs + virtual IPs), and firewall (DNAT/SNAT bridges).
type Manager struct {
	cfg       config.InternalNodeConfig
	bgpServer bgplib.Server
	logger    *zap.Logger
}

// NewManager constructs a Manager. bgpServer must already be started by the
// caller (cmd/agent/main.go) before Run is invoked.
func NewManager(cfg config.InternalNodeConfig, bgpServer bgplib.Server, logger *zap.Logger) *Manager {
	return &Manager{cfg: cfg, bgpServer: bgpServer, logger: logger}
}

// Run wires all internal-node components and blocks until ctx is cancelled.
func (m *Manager) Run(ctx context.Context) error {
	m.logger.Info("internal-node manager started",
		zap.String("nodeName", m.cfg.NodeName),
		zap.String("firewall", m.cfg.Firewall),
	)

	obs := cluster.NewObserver(m.logger)
	med := cluster.NewMediator(obs, m.logger)

	bgpReconciler := bgprecon.NewInternalReconciler(
		m.bgpServer, m.cfg.ASN, m.cfg.RouterID, m.cfg.NodeName, m.logger,
	)

	var fw firewall.Firewall
	switch m.cfg.Firewall {
	case "nftables":
		fw = firewall.NewNFTables()
	default:
		fw = firewall.NewIPTables()
	}

	routesReconciler := routesrecon.NewInternalReconciler(
		routeslib.NewNetlinkRoutes(), fw, m.cfg.NodeName, m.logger,
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
	m.logger.Info("internal-node manager stopped")
	return nil
}
