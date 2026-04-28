package wireguard

import (
	"context"

	"go.uber.org/zap"

	v1alpha1 "github.com/tsamsiyu/pontifex/api/v1alpha1"
	"github.com/tsamsiyu/pontifex/apps/agent/internal/libs/wg"
)

// Reconciler ensures one WireGuard interface per overlay on a gateway host.
// The private key is loaded at reconcile time from
// <wgKeyDir>/<overlay>/private (mounted by the operator from the per-overlay
// Secret referenced in status.wgSecretRef).
type Reconciler struct {
	logger   *zap.Logger
	wg       wg.Manager
	wgKeyDir string
	listen   int
}

// New returns a Reconciler.
func New(mgr wg.Manager, wgKeyDir string, listenPort int, logger *zap.Logger) *Reconciler {
	return &Reconciler{logger: logger, wg: mgr, wgKeyDir: wgKeyDir, listen: listenPort}
}

// Reconcile is a Phase 1 stub.
func (r *Reconciler) Reconcile(ctx context.Context, overlays []v1alpha1.NetworkOverlay) error {
	return nil
}
