package routes

import (
	"context"
	"errors"
	"testing"

	v1alpha1 "github.com/tsamsiyu/pontifex/api/v1alpha1"
)

// ── no-op ─────────────────────────────────────────────────────────────────────

func TestGateway_NoOverlaysNoLearnedRoutes(t *testing.T) {
	f := newFakeRoutes()
	r := newGatewayReconcilerForTest(f)

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

// ── ApplyEvent ────────────────────────────────────────────────────────────────

func TestGateway_ApplyEvent_RouteAddedIncludedInSync(t *testing.T) {
	f := newFakeRoutes()
	r := newGatewayReconcilerForTest(f)
	r.ApplyEvent(addedEvent("10.1.0.0/24", "192.168.0.1", "65000:1"))

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

func TestGateway_ApplyEvent_RouteWithdrawnRemovedFromSync(t *testing.T) {
	f := newFakeRoutes()
	r := newGatewayReconcilerForTest(f)
	r.ApplyEvent(addedEvent("10.1.0.0/24", "192.168.0.1", "65000:1"))
	r.ApplyEvent(withdrawnEvent("10.1.0.0/24"))

	ov := mkOverlay("ov", "65000:1", "", nil, nil)
	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sc, _ := hasSyncCall(f.syncRoutesCalls, 0)
	if syncDesiredContains(sc.desired, "10.1.0.0/24") {
		t.Error("withdrawn route must not appear in desired")
	}
}

func TestGateway_ApplyEvent_MultipleRoutesAccumulate(t *testing.T) {
	f := newFakeRoutes()
	r := newGatewayReconcilerForTest(f)
	r.ApplyEvent(addedEvent("10.1.0.0/24", "1.1.1.1", "65000:1"))
	r.ApplyEvent(addedEvent("10.2.0.0/24", "1.1.1.1", "65000:1"))
	r.ApplyEvent(addedEvent("10.3.0.0/24", "1.1.1.1", "65000:1"))

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
	r := newGatewayReconcilerForTest(f)
	r.ApplyEvent(addedEvent("10.1.0.0/24", "1.1.1.1", "65000:1"))

	// First reconcile with overlay present.
	ov := mkOverlay("ov", "65000:1", "", nil, nil)
	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}

	// Second reconcile: overlay gone — route filtered out.
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
	r := newGatewayReconcilerForTest(f)
	r.ApplyEvent(addedEvent("10.1.0.0/24", "1.1.1.1")) // no community

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
	r := newGatewayReconcilerForTest(f)
	r.ApplyEvent(addedEvent("10.1.0.0/24", "1.1.1.1", "65000:1"))

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
	r := newGatewayReconcilerForTest(f)
	r.ApplyEvent(addedEvent("10.1.0.0/24", "1.1.1.1", "65000:1"))

	ov := mkOverlay("ov", "65000:1", "", nil, nil)

	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}

	// Second call with no new events — learned map still has the route.
	f.syncRoutesCalls = nil
	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}

	sc, _ := hasSyncCall(f.syncRoutesCalls, 0)
	if !syncDesiredContains(sc.desired, "10.1.0.0/24") {
		t.Error("learned route must persist across reconcile calls")
	}
}

// ── error propagation ─────────────────────────────────────────────────────────

func TestGateway_SyncRoutesError(t *testing.T) {
	syncErr := errors.New("netlink write failed")
	f := newFakeRoutes()
	f.syncRoutesErr = syncErr
	r := newGatewayReconcilerForTest(f)

	err := r.Reconcile(context.Background(), nil)
	if !errors.Is(err, syncErr) {
		t.Errorf("err=%v, want to wrap %v", err, syncErr)
	}
}
