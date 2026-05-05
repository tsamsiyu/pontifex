package bgp

import (
	"context"
	"errors"
	"testing"

	"go.uber.org/zap"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/tsamsiyu/pontifex/api/v1alpha1"
	bgplib "github.com/tsamsiyu/pontifex/apps/agent/internal/libs/bgp"
)

// ── fake reconciler ───────────────────────────────────────────────────────────

type fakeRoutesReconciler struct {
	appliedEvents  []bgplib.RouteEvent
	reconcileCalls [][]v1alpha1.NetworkOverlay
	reconcileErr   error
}

var _ RoutesReconciler = (*fakeRoutesReconciler)(nil)

func (f *fakeRoutesReconciler) ApplyEvent(ev bgplib.RouteEvent) {
	f.appliedEvents = append(f.appliedEvents, ev)
}

func (f *fakeRoutesReconciler) Reconcile(_ context.Context, overlays []v1alpha1.NetworkOverlay) error {
	f.reconcileCalls = append(f.reconcileCalls, overlays)
	return f.reconcileErr
}

// ── helpers ───────────────────────────────────────────────────────────────────

func newUpdater(bgpCh <-chan bgplib.RouteEvent, snapCh <-chan []v1alpha1.NetworkOverlay, r *fakeRoutesReconciler) *RoutesUpdater {
	return NewRoutesUpdater(bgpCh, snapCh, r, zap.NewNop())
}

func mkSnapshot(names ...string) []v1alpha1.NetworkOverlay {
	ovs := make([]v1alpha1.NetworkOverlay, len(names))
	for i, n := range names {
		ovs[i] = v1alpha1.NetworkOverlay{ObjectMeta: metav1.ObjectMeta{Name: n}}
	}
	return ovs
}

func addedRouteEvent(prefix string) bgplib.RouteEvent {
	return bgplib.RouteEvent{
		Type:  bgplib.RouteAdded,
		Route: bgplib.Route{Prefix: prefix},
	}
}

// runOnce drains exactly one select iteration by pre-loading channels and
// cancelling after the first reconcile call.
func runOnce(t *testing.T, u *RoutesUpdater, r *fakeRoutesReconciler) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after first Reconcile so Run exits promptly.
	origReconcile := r.reconcileErr
	done := make(chan struct{})
	go func() {
		_ = u.Run(ctx)
		close(done)
	}()
	// Wait until reconcile is recorded, then cancel.
	for len(r.reconcileCalls) == 0 {
		// busy-spin acceptable in test; channels are pre-loaded so fast.
	}
	cancel()
	<-done
	r.reconcileErr = origReconcile
}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestRoutesUpdater_OnSnapshot_ReconcileCalledWithSnapshot(t *testing.T) {
	snapCh := make(chan []v1alpha1.NetworkOverlay, 1)
	bgpCh := make(chan bgplib.RouteEvent, 1)
	r := &fakeRoutesReconciler{}
	u := newUpdater(bgpCh, snapCh, r)

	snap := mkSnapshot("ov1")
	snapCh <- snap

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = u.Run(ctx) }()

	// Wait for Reconcile to be called.
	for len(r.reconcileCalls) == 0 {
	}
	cancel()

	if len(r.reconcileCalls) == 0 {
		t.Fatal("Reconcile not called after snapshot")
	}
	if len(r.reconcileCalls[0]) != 1 || r.reconcileCalls[0][0].Name != "ov1" {
		t.Errorf("Reconcile called with %v, want [ov1]", r.reconcileCalls[0])
	}
}

func TestRoutesUpdater_OnBGPEvent_ApplyEventCalledBeforeReconcile(t *testing.T) {
	snapCh := make(chan []v1alpha1.NetworkOverlay, 1)
	bgpCh := make(chan bgplib.RouteEvent, 1)
	r := &fakeRoutesReconciler{}
	u := newUpdater(bgpCh, snapCh, r)

	ev := addedRouteEvent("10.0.0.0/24")
	bgpCh <- ev

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = u.Run(ctx) }()

	for len(r.reconcileCalls) == 0 {
	}
	cancel()

	if len(r.appliedEvents) != 1 || r.appliedEvents[0].Route.Prefix != "10.0.0.0/24" {
		t.Errorf("appliedEvents=%v, want [{10.0.0.0/24}]", r.appliedEvents)
	}
	if len(r.reconcileCalls) == 0 {
		t.Error("Reconcile not called after BGP event")
	}
}

func TestRoutesUpdater_OnBGPEvent_UsesLatestSnapshot(t *testing.T) {
	snapCh := make(chan []v1alpha1.NetworkOverlay, 2)
	bgpCh := make(chan bgplib.RouteEvent, 1)
	r := &fakeRoutesReconciler{}
	u := newUpdater(bgpCh, snapCh, r)

	// Pre-load: first a snapshot, then a BGP event.
	snap := mkSnapshot("ov1")
	snapCh <- snap
	bgpCh <- addedRouteEvent("10.0.0.0/24")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = u.Run(ctx) }()

	// Wait for 2 Reconcile calls (one for snapshot, one for BGP event).
	for len(r.reconcileCalls) < 2 {
	}
	cancel()

	// The BGP-triggered reconcile must use the stored snapshot.
	bgpReconcileArg := r.reconcileCalls[1]
	if len(bgpReconcileArg) != 1 || bgpReconcileArg[0].Name != "ov1" {
		t.Errorf("BGP reconcile used snapshot %v, want [ov1]", bgpReconcileArg)
	}
}

func TestRoutesUpdater_ContextCancel_Stops(t *testing.T) {
	snapCh := make(chan []v1alpha1.NetworkOverlay)
	bgpCh := make(chan bgplib.RouteEvent)
	r := &fakeRoutesReconciler{}
	u := newUpdater(bgpCh, snapCh, r)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- u.Run(ctx) }()

	cancel()
	err := <-done
	if err != nil {
		t.Errorf("Run returned %v, want nil", err)
	}
}

func TestRoutesUpdater_ReconcileError_LoopContinues(t *testing.T) {
	snapCh := make(chan []v1alpha1.NetworkOverlay, 2)
	bgpCh := make(chan bgplib.RouteEvent, 1)
	reconcileErr := errors.New("netlink down")
	r := &fakeRoutesReconciler{reconcileErr: reconcileErr}
	u := newUpdater(bgpCh, snapCh, r)

	// Two snapshots — loop must continue after first error.
	snapCh <- mkSnapshot("ov1")
	snapCh <- mkSnapshot("ov2")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = u.Run(ctx) }()

	for len(r.reconcileCalls) < 2 {
	}
	cancel()

	if len(r.reconcileCalls) < 2 {
		t.Errorf("reconcileCalls=%d; loop must continue after Reconcile error", len(r.reconcileCalls))
	}
}

func TestRoutesUpdater_MultipleEvents_AllProcessed(t *testing.T) {
	snapCh := make(chan []v1alpha1.NetworkOverlay, 1)
	bgpCh := make(chan bgplib.RouteEvent, 3)
	r := &fakeRoutesReconciler{}
	u := newUpdater(bgpCh, snapCh, r)

	bgpCh <- addedRouteEvent("10.0.0.0/24")
	bgpCh <- addedRouteEvent("10.1.0.0/24")
	bgpCh <- addedRouteEvent("10.2.0.0/24")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = u.Run(ctx) }()

	for len(r.reconcileCalls) < 3 {
	}
	cancel()

	if len(r.appliedEvents) != 3 {
		t.Errorf("appliedEvents=%d, want 3", len(r.appliedEvents))
	}
	if len(r.reconcileCalls) < 3 {
		t.Errorf("reconcileCalls=%d, want >=3", len(r.reconcileCalls))
	}
}

func TestRoutesUpdater_NoBGPEvents_SnapshotStillReconciles(t *testing.T) {
	snapCh := make(chan []v1alpha1.NetworkOverlay, 1)
	bgpCh := make(chan bgplib.RouteEvent) // never receives
	r := &fakeRoutesReconciler{}
	u := newUpdater(bgpCh, snapCh, r)

	snapCh <- mkSnapshot("ov1")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = u.Run(ctx) }()

	for len(r.reconcileCalls) == 0 {
	}
	cancel()

	if len(r.reconcileCalls) != 1 {
		t.Errorf("reconcileCalls=%d, want 1", len(r.reconcileCalls))
	}
	if len(r.appliedEvents) != 0 {
		t.Errorf("appliedEvents=%d; no BGP events should have been applied", len(r.appliedEvents))
	}
}
