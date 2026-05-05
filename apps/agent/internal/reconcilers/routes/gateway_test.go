package routes

import (
	"context"
	"errors"
	"testing"

	"go.uber.org/zap"

	v1alpha1 "github.com/tsamsiyu/pontifex/api/v1alpha1"
	"github.com/tsamsiyu/pontifex/apps/agent/internal/libs/bgp"
)

func newGatewayReconciler(f *fakeRoutes, events <-chan bgp.RouteEvent) *GatewayReconciler {
	return NewGatewayReconciler(f, events, zap.NewNop())
}

// ── no-op ─────────────────────────────────────────────────────────────────────

func TestGateway_NoOverlaysNoEvents(t *testing.T) {
	f := newFakeRoutes()
	r := newGatewayReconciler(f, mkBGPEvents())

	if err := r.Reconcile(context.Background(), nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.syncRoutesCalls) != 1 {
		t.Fatalf("syncRoutesCalls=%d, want 1", len(f.syncRoutesCalls))
	}
	if len(f.syncRoutesCalls[0].desired) != 0 {
		t.Errorf("desired=%v, want empty", f.syncRoutesCalls[0].desired)
	}
}

// ── BGP event draining ────────────────────────────────────────────────────────

func TestGateway_RouteAddedEventIncludedInSync(t *testing.T) {
	f := newFakeRoutes()
	ch := mkBGPEvents(addedRoute("10.1.0.0/24", "192.168.0.1", "65000:1"))
	r := newGatewayReconciler(f, ch)

	ov := mkOverlay("ov", "65000:1", "", nil, nil)
	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sc, ok := hasSyncCall(f.syncRoutesCalls, 0)
	if !ok {
		t.Fatal("no SyncRoutes call for table 0")
	}
	if !syncDesiredContains(sc.desired, "10.1.0.0/24") {
		t.Errorf("desired=%v, missing 10.1.0.0/24", sc.desired)
	}
}

func TestGateway_RouteWithdrawnRemovedFromSync(t *testing.T) {
	f := newFakeRoutes()
	ch := mkBGPEvents(
		addedRoute("10.1.0.0/24", "192.168.0.1", "65000:1"),
		withdrawnRoute("10.1.0.0/24"),
	)
	r := newGatewayReconciler(f, ch)

	ov := mkOverlay("ov", "65000:1", "", nil, nil)
	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sc, _ := hasSyncCall(f.syncRoutesCalls, 0)
	if syncDesiredContains(sc.desired, "10.1.0.0/24") {
		t.Error("withdrawn route must not appear in desired")
	}
}

func TestGateway_MultipleEventsAllDrained(t *testing.T) {
	f := newFakeRoutes()
	ch := mkBGPEvents(
		addedRoute("10.1.0.0/24", "1.1.1.1", "65000:1"),
		addedRoute("10.2.0.0/24", "1.1.1.1", "65000:1"),
		addedRoute("10.3.0.0/24", "1.1.1.1", "65000:1"),
	)
	r := newGatewayReconciler(f, ch)

	ov := mkOverlay("ov", "65000:1", "", nil, nil)
	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sc, _ := hasSyncCall(f.syncRoutesCalls, 0)
	if len(sc.desired) != 3 {
		t.Errorf("desired=%d routes, want 3", len(sc.desired))
	}
}

// ── community filtering ───────────────────────────────────────────────────────

func TestGateway_LearnedRouteFilteredWhenOverlayRemoved(t *testing.T) {
	f := newFakeRoutes()
	ch := mkBGPEvents(addedRoute("10.1.0.0/24", "1.1.1.1", "65000:1"))
	r := newGatewayReconciler(f, ch)

	// First reconcile with overlay present — drains event into learned.
	ov := mkOverlay("ov", "65000:1", "", nil, nil)
	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}

	// Second reconcile: overlay is gone — route must be filtered out.
	f.syncRoutesCalls = nil
	if err := r.Reconcile(context.Background(), nil); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}

	sc, _ := hasSyncCall(f.syncRoutesCalls, 0)
	if syncDesiredContains(sc.desired, "10.1.0.0/24") {
		t.Error("route must be filtered when overlay community is no longer active")
	}
}

func TestGateway_RouteWithNoCommunityNotIncluded(t *testing.T) {
	f := newFakeRoutes()
	// Route has no community — should never match any overlay.
	ch := mkBGPEvents(addedRoute("10.1.0.0/24", "1.1.1.1"))
	r := newGatewayReconciler(f, ch)

	ov := mkOverlay("ov", "65000:1", "", nil, nil)
	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sc, _ := hasSyncCall(f.syncRoutesCalls, 0)
	if syncDesiredContains(sc.desired, "10.1.0.0/24") {
		t.Error("route without matching community must not be included")
	}
}

func TestGateway_OverlayWithEmptyCommunityDoesNotMatchRoutes(t *testing.T) {
	f := newFakeRoutes()
	ch := mkBGPEvents(addedRoute("10.1.0.0/24", "1.1.1.1", "65000:1"))
	r := newGatewayReconciler(f, ch)

	ov := mkOverlay("ov", "", "", nil, nil)
	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sc, _ := hasSyncCall(f.syncRoutesCalls, 0)
	if syncDesiredContains(sc.desired, "10.1.0.0/24") {
		t.Error("overlay with empty community must not match any learned route")
	}
}

// ── persistence across reconcile calls ───────────────────────────────────────

func TestGateway_LearnedStateRetainedBetweenCalls(t *testing.T) {
	f := newFakeRoutes()
	ch := mkBGPEvents(addedRoute("10.1.0.0/24", "1.1.1.1", "65000:1"))
	r := newGatewayReconciler(f, ch)

	ov := mkOverlay("ov", "65000:1", "", nil, nil)

	// First call drains the event.
	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}

	// Second call: no new events; learned map still has the route.
	f.syncRoutesCalls = nil
	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}

	sc, _ := hasSyncCall(f.syncRoutesCalls, 0)
	if !syncDesiredContains(sc.desired, "10.1.0.0/24") {
		t.Error("learned route must persist across reconcile calls without new events")
	}
}

// ── error propagation ─────────────────────────────────────────────────────────

func TestGateway_SyncRoutesError(t *testing.T) {
	syncErr := errors.New("netlink write failed")
	f := newFakeRoutes()
	f.syncRoutesErr = syncErr
	r := newGatewayReconciler(f, mkBGPEvents())

	err := r.Reconcile(context.Background(), nil)
	if !errors.Is(err, syncErr) {
		t.Errorf("err=%v, want to wrap %v", err, syncErr)
	}
}
