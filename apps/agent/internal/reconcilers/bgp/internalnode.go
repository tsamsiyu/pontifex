package bgp

import (
	"context"

	"go.uber.org/zap"

	v1alpha1 "github.com/tsamsiyu/pontifex/api/v1alpha1"
	"github.com/tsamsiyu/pontifex/apps/agent/internal/libs/bgp"
)

// InternalReconciler runs a GoBGP server for the internal-node role: iBGP to
// every entry in status.gateways, advertising local edge VirtualIPs with the
// overlay community and importing peer-learned routes into per-overlay VRFs.
type InternalReconciler struct {
	logger   *zap.Logger
	server   bgp.Server
	nodeName string
}

// NewInternalReconciler returns an InternalReconciler.
func NewInternalReconciler(server bgp.Server, nodeName string, logger *zap.Logger) *InternalReconciler {
	return &InternalReconciler{logger: logger, server: server, nodeName: nodeName}
}

// Reconcile is a Phase 1 stub.
func (r *InternalReconciler) Reconcile(ctx context.Context, overlays []v1alpha1.NetworkOverlay) error {
	return nil
}
