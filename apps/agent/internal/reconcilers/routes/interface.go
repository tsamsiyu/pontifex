package routes

import (
	"context"

	v1alpha1 "github.com/tsamsiyu/pontifex/api/v1alpha1"
)

// Reconciler installs host routing state (VRFs, addrs, ip rules, routes)
// derived from overlay topology + BGP route events.
type Reconciler interface {
	Reconcile(ctx context.Context, overlays []v1alpha1.NetworkOverlay) error
}
