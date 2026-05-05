package bgp

import (
	"context"

	"go.uber.org/zap"

	v1alpha1 "github.com/tsamsiyu/pontifex/api/v1alpha1"
	bgplib "github.com/tsamsiyu/pontifex/apps/agent/internal/libs/bgp"
)

// RoutesReconciler is implemented by both the gateway and internal-node routes
// reconcilers. ApplyEvent updates the reconciler's learned-route state;
// Reconcile converges host networking to match the overlay snapshot.
type RoutesReconciler interface {
	ApplyEvent(ev bgplib.RouteEvent)
	Reconcile(ctx context.Context, overlays []v1alpha1.NetworkOverlay) error
}

// RoutesUpdater drives the routes reconciler from two independent event
// sources: mediator overlay snapshots (topology changes) and BGP route events
// (near-realtime route learn/withdraw). Either trigger causes a full
// idempotent reconcile against the latest known snapshot.
type RoutesUpdater struct {
	logger     *zap.Logger
	bgpEvents  <-chan bgplib.RouteEvent
	snapshots  <-chan []v1alpha1.NetworkOverlay
	reconciler RoutesReconciler
}

// NewRoutesUpdater returns a RoutesUpdater.
func NewRoutesUpdater(
	bgpEvents <-chan bgplib.RouteEvent,
	snapshots <-chan []v1alpha1.NetworkOverlay,
	r RoutesReconciler,
	logger *zap.Logger,
) *RoutesUpdater {
	return &RoutesUpdater{
		logger:     logger,
		bgpEvents:  bgpEvents,
		snapshots:  snapshots,
		reconciler: r,
	}
}

// Run blocks, selecting on bgpEvents, snapshots, and ctx.Done().
//
//   - On mediator snapshot: stores it as the latest snapshot, then reconciles.
//   - On BGP route event:   calls ApplyEvent to update learned state, then
//     reconciles against the latest snapshot.
//
// Reconcile errors are logged but never stop the loop — transient failures are
// expected and the next event triggers a fresh attempt.
func (u *RoutesUpdater) Run(ctx context.Context) error {
	var latest []v1alpha1.NetworkOverlay
	for {
		select {
		case snapshot, ok := <-u.snapshots:
			if !ok {
				return nil
			}
			latest = snapshot
			if err := u.reconciler.Reconcile(ctx, latest); err != nil {
				u.logger.Error("routes reconcile on snapshot", zap.Error(err))
			}
		case ev, ok := <-u.bgpEvents:
			if !ok {
				return nil
			}
			u.reconciler.ApplyEvent(ev)
			if err := u.reconciler.Reconcile(ctx, latest); err != nil {
				u.logger.Error("routes reconcile on bgp event", zap.Error(err))
			}
		case <-ctx.Done():
			return nil
		}
	}
}
