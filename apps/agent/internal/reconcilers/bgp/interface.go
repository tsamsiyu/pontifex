package bgp

import (
	"context"

	v1alpha1 "github.com/tsamsiyu/pontifex/api/v1alpha1"
)

// Reconciler applies overlay state to the local BGP daemon. Each call receives
// the latest snapshot of all NetworkOverlay CRs; reconcilers filter for
// themselves.
type Reconciler interface {
	Reconcile(ctx context.Context, overlays []v1alpha1.NetworkOverlay) error
}
