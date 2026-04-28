package gateways

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// OverlayUpdater subscribes to the Observer and, on each snapshot, patches
// status.gateways on every NetworkOverlay whose current value differs from
// the snapshot. Uses a status-subresource MergeFrom patch and is a no-op on
// equality to avoid churn.
type OverlayUpdater struct {
	Client   client.Client
	Observer *Observer
}

// Run subscribes and patches forever. Phase 1 stub.
func (u *OverlayUpdater) Run(ctx context.Context) error {
	<-ctx.Done()
	return nil
}
