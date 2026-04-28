package routes

import (
	"context"

	"go.uber.org/zap"

	v1alpha1 "github.com/tsamsiyu/pontifex/api/v1alpha1"
	"github.com/tsamsiyu/pontifex/apps/agent/internal/libs/routes"
)

// GatewayReconciler installs peer-learned routes globally on the gateway host.
// Gateways do not host pods so VRF-per-overlay is unnecessary.
type GatewayReconciler struct {
	logger *zap.Logger
	routes routes.Routes
}

// NewGatewayReconciler returns a GatewayReconciler.
func NewGatewayReconciler(rt routes.Routes, logger *zap.Logger) *GatewayReconciler {
	return &GatewayReconciler{logger: logger, routes: rt}
}

// Reconcile is a Phase 1 stub.
func (r *GatewayReconciler) Reconcile(ctx context.Context, overlays []v1alpha1.NetworkOverlay) error {
	return nil
}
