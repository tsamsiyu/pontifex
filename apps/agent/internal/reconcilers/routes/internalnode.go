package routes

import (
	"context"

	"go.uber.org/zap"

	v1alpha1 "github.com/tsamsiyu/pontifex/api/v1alpha1"
	"github.com/tsamsiyu/pontifex/apps/agent/internal/libs/firewall"
	"github.com/tsamsiyu/pontifex/apps/agent/internal/libs/routes"
)

// InternalReconciler manages per-overlay VRFs, virtual-IP addresses, ip rules,
// BGP-learned routes, and the iptables/nftables bridge that DNATs virtual IPs
// to local pod IPs.
type InternalReconciler struct {
	logger   *zap.Logger
	routes   routes.Routes
	firewall firewall.Firewall
	nodeName string
}

// NewInternalReconciler returns an InternalReconciler.
func NewInternalReconciler(rt routes.Routes, fw firewall.Firewall, nodeName string, logger *zap.Logger) *InternalReconciler {
	return &InternalReconciler{logger: logger, routes: rt, firewall: fw, nodeName: nodeName}
}

// Reconcile is a Phase 1 stub.
func (r *InternalReconciler) Reconcile(ctx context.Context, overlays []v1alpha1.NetworkOverlay) error {
	return nil
}
