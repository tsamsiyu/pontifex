package bgp

import (
	"context"

	"go.uber.org/zap"

	v1alpha1 "github.com/tsamsiyu/pontifex/api/v1alpha1"
	"github.com/tsamsiyu/pontifex/apps/agent/internal/libs/bgp"
)

// GatewayReconciler runs a GoBGP server for the gateway role: route reflector
// to internal-node iBGP peers, eBGP to external peers over WireGuard, with
// per-overlay community import/export filters.
type GatewayReconciler struct {
	logger *zap.Logger
	server bgp.Server
}

// NewGatewayReconciler returns a GatewayReconciler.
func NewGatewayReconciler(server bgp.Server, logger *zap.Logger) *GatewayReconciler {
	return &GatewayReconciler{logger: logger, server: server}
}

// Reconcile is a Phase 1 stub.
func (r *GatewayReconciler) Reconcile(ctx context.Context, overlays []v1alpha1.NetworkOverlay) error {
	return nil
}
