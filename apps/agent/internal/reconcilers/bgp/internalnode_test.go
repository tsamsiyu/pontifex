package bgp

import (
	"context"
	"errors"
	"testing"

	"go.uber.org/zap"

	v1alpha1 "github.com/tsamsiyu/pontifex/api/v1alpha1"
	"github.com/tsamsiyu/pontifex/apps/agent/internal/libs/bgp"
)

func newInternalReconciler(f *fakeServer, nodeName string) *InternalReconciler {
	return NewInternalReconciler(f, 65000, "10.0.1.5", nodeName, zap.NewNop())
}

// ── server lifecycle ──────────────────────────────────────────────────────────

func TestInternalReconcile_StartCalledOnce(t *testing.T) {
	f := newFakeServer()
	r := newInternalReconciler(f, "node1")
	for i := 0; i < 3; i++ {
		if err := r.Reconcile(context.Background(), nil); err != nil {
			t.Fatalf("reconcile %d: %v", i, err)
		}
	}
	if f.startCalls != 1 {
		t.Errorf("startCalls=%d after 3 reconciles, want 1", f.startCalls)
	}
}

func TestInternalReconcile_StartError(t *testing.T) {
	startErr := errors.New("daemon not running")
	f := newFakeServer()
	f.startErr = startErr
	r := newInternalReconciler(f, "node1")

	err := r.Reconcile(context.Background(), nil)
	if !errors.Is(err, startErr) {
		t.Errorf("err=%v, want to wrap %v", err, startErr)
	}
	if len(f.addNeighborCalls) != 0 || len(f.addPolicyCalls) != 0 || len(f.advertiseCalls) != 0 {
		t.Errorf("unexpected calls after start error")
	}
}

// ── no overlays ───────────────────────────────────────────────────────────────

func TestInternalReconcile_NoOverlays(t *testing.T) {
	f := newFakeServer()
	r := newInternalReconciler(f, "node1")
	if err := r.Reconcile(context.Background(), nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.addNeighborCalls) != 0 || len(f.addPolicyCalls) != 0 || len(f.advertiseCalls) != 0 {
		t.Errorf("expected no calls; addNeighbor=%d addPolicy=%d advertise=%d",
			len(f.addNeighborCalls), len(f.addPolicyCalls), len(f.advertiseCalls))
	}
}

// ── iBGP neighbor to gateway ──────────────────────────────────────────────────

func TestInternalReconcile_IBGPNeighborToGateway(t *testing.T) {
	f := newFakeServer()
	r := newInternalReconciler(f, "node1")

	ov := mkOverlay("ov", "65000:1", nil,
		[]v1alpha1.Gateway{{NodeIP: "10.0.0.1"}},
		nil)

	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	n, ok := hasNeighbor(f.addNeighborCalls, "10.0.0.1")
	if !ok {
		t.Fatalf("iBGP neighbor 10.0.0.1 not added; calls=%+v", f.addNeighborCalls)
	}
	if !n.IsIBGP {
		t.Error("IsIBGP=false, want true for gateway iBGP neighbor")
	}
	if n.ASN != 65000 {
		t.Errorf("ASN=%d, want 65000 (local ASN)", n.ASN)
	}
	if n.Name != "pntfx-gateway-10.0.0.1" {
		t.Errorf("Name=%q, want pntfx-gateway-10.0.0.1", n.Name)
	}
}

func TestInternalReconcile_GatewayDeduplicatedAcrossOverlays(t *testing.T) {
	f := newFakeServer()
	r := newInternalReconciler(f, "node1")

	gw := v1alpha1.Gateway{NodeIP: "10.0.0.1"}
	ov1 := mkOverlay("ov1", "65000:1", nil, []v1alpha1.Gateway{gw}, nil)
	ov2 := mkOverlay("ov2", "65000:2", nil, []v1alpha1.Gateway{gw}, nil)

	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov1, ov2}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	count := 0
	for _, n := range f.addNeighborCalls {
		if n.Address == "10.0.0.1" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("gateway neighbor 10.0.0.1 added %d times, want 1", count)
	}
}

func TestInternalReconcile_EmptyGatewayNodeIPSkipped(t *testing.T) {
	f := newFakeServer()
	r := newInternalReconciler(f, "node1")

	ov := mkOverlay("ov", "65000:1", nil,
		[]v1alpha1.Gateway{{NodeIP: ""}},
		nil)

	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.addNeighborCalls) != 0 {
		t.Errorf("addNeighborCalls=%d; empty NodeIP must be skipped", len(f.addNeighborCalls))
	}
}

// ── advertisement ─────────────────────────────────────────────────────────────

func TestInternalReconcile_EdgeAdvertisedForThisNode(t *testing.T) {
	f := newFakeServer()
	r := newInternalReconciler(f, "node1")

	ov := mkOverlay("ov", "65000:1", nil, nil,
		[]v1alpha1.EdgeStatus{{
			NodeName:  "node1",
			VirtualIP: "192.168.1.10",
			Ready:     true,
		}},
	)

	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(f.advertiseCalls) != 1 {
		t.Fatalf("advertiseCalls=%d, want 1", len(f.advertiseCalls))
	}
	ad := f.advertiseCalls[0]
	if ad.Prefix != "192.168.1.10/32" {
		t.Errorf("Prefix=%q, want 192.168.1.10/32", ad.Prefix)
	}
	if !containsStr(ad.Communities, "65000:1") {
		t.Errorf("Communities=%v, missing 65000:1", ad.Communities)
	}
}

func TestInternalReconcile_EdgeOnOtherNodeNotAdvertised(t *testing.T) {
	f := newFakeServer()
	r := newInternalReconciler(f, "node1")

	ov := mkOverlay("ov", "65000:1", nil, nil,
		[]v1alpha1.EdgeStatus{{
			NodeName:  "node2", // different node
			VirtualIP: "192.168.1.10",
			Ready:     true,
		}},
	)

	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.advertiseCalls) != 0 {
		t.Errorf("advertiseCalls=%d; edge on other node must not be advertised", len(f.advertiseCalls))
	}
}

func TestInternalReconcile_EdgeNotReadyNotAdvertised(t *testing.T) {
	f := newFakeServer()
	r := newInternalReconciler(f, "node1")

	ov := mkOverlay("ov", "65000:1", nil, nil,
		[]v1alpha1.EdgeStatus{{
			NodeName:  "node1",
			VirtualIP: "192.168.1.10",
			Ready:     false,
		}},
	)

	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.advertiseCalls) != 0 {
		t.Errorf("advertiseCalls=%d; non-ready edge must not be advertised", len(f.advertiseCalls))
	}
}

func TestInternalReconcile_EdgeEmptyVIPSkipped(t *testing.T) {
	f := newFakeServer()
	r := newInternalReconciler(f, "node1")

	ov := mkOverlay("ov", "65000:1", nil, nil,
		[]v1alpha1.EdgeStatus{{
			NodeName:  "node1",
			VirtualIP: "",
			Ready:     true,
		}},
	)

	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.advertiseCalls) != 0 {
		t.Errorf("advertiseCalls=%d; empty VirtualIP must be skipped", len(f.advertiseCalls))
	}
}

func TestInternalReconcile_MultipleEdgesOnThisNode(t *testing.T) {
	f := newFakeServer()
	r := newInternalReconciler(f, "node1")

	ov := mkOverlay("ov", "65000:1", nil, nil,
		[]v1alpha1.EdgeStatus{
			{NodeName: "node1", VirtualIP: "192.168.1.10", Ready: true},
			{NodeName: "node1", VirtualIP: "192.168.1.11", Ready: true},
			{NodeName: "node2", VirtualIP: "192.168.1.12", Ready: true}, // other node
		},
	)

	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.advertiseCalls) != 2 {
		t.Errorf("advertiseCalls=%d, want 2 (only this node's edges)", len(f.advertiseCalls))
	}
}

// ── policies ──────────────────────────────────────────────────────────────────

func TestInternalReconcile_PoliciesCreatedForOverlay(t *testing.T) {
	f := newFakeServer()
	r := newInternalReconciler(f, "node1")

	ov := mkOverlay("myov", "65000:7", nil, nil, nil)
	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	exp, ok := hasPolicy(f.addPolicyCalls, "pntfx-export-myov")
	if !ok {
		t.Fatal("export policy not added")
	}
	if exp.Direction != bgp.PolicyExport {
		t.Errorf("export Direction=%v, want PolicyExport", exp.Direction)
	}
	if exp.Action != "community-set:65000:7" {
		t.Errorf("export Action=%q, want community-set:65000:7", exp.Action)
	}

	imp, ok := hasPolicy(f.addPolicyCalls, "pntfx-import-myov")
	if !ok {
		t.Fatal("import policy not added")
	}
	if imp.Direction != bgp.PolicyImport {
		t.Errorf("import Direction=%v, want PolicyImport", imp.Direction)
	}
	if imp.Match != "community:65000:7" {
		t.Errorf("import Match=%q, want community:65000:7", imp.Match)
	}
}

// ── empty community skipped ───────────────────────────────────────────────────

func TestInternalReconcile_OverlayWithEmptyCommunitySkipped(t *testing.T) {
	f := newFakeServer()
	r := newInternalReconciler(f, "node1")

	ov := mkOverlay("ov", "", nil,
		[]v1alpha1.Gateway{{NodeIP: "10.0.0.1"}},
		[]v1alpha1.EdgeStatus{{NodeName: "node1", VirtualIP: "192.168.1.10", Ready: true}},
	)

	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Gateways are added regardless of community (neighbor is community-agnostic).
	// Policies and advertisements require a community.
	if len(f.addPolicyCalls) != 0 {
		t.Errorf("addPolicyCalls=%d; policies must be skipped for empty community", len(f.addPolicyCalls))
	}
	if len(f.advertiseCalls) != 0 {
		t.Errorf("advertiseCalls=%d; advertisements must be skipped for empty community", len(f.advertiseCalls))
	}
}

// ── orphan removal ────────────────────────────────────────────────────────────

func TestInternalReconcile_OrphanNeighborRemoved(t *testing.T) {
	f := newFakeServer()
	f.neighbors = []bgp.Neighbor{{Address: "10.99.99.99"}}
	r := newInternalReconciler(f, "node1")

	if err := r.Reconcile(context.Background(), nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.removeNeighborCalls) != 1 || f.removeNeighborCalls[0] != "10.99.99.99" {
		t.Errorf("removeNeighborCalls=%v, want [10.99.99.99]", f.removeNeighborCalls)
	}
}

func TestInternalReconcile_OrphanPolicyRemoved(t *testing.T) {
	f := newFakeServer()
	f.policies = []bgp.Policy{{Name: "pntfx-export-stale"}}
	r := newInternalReconciler(f, "node1")

	if err := r.Reconcile(context.Background(), nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.removePolicyCalls) != 1 || f.removePolicyCalls[0] != "pntfx-export-stale" {
		t.Errorf("removePolicyCalls=%v, want [pntfx-export-stale]", f.removePolicyCalls)
	}
}

func TestInternalReconcile_OrphanAdvertisementWithdrawn(t *testing.T) {
	f := newFakeServer()
	f.advertised = []bgp.Route{{Prefix: "192.168.9.9/32"}}
	r := newInternalReconciler(f, "node1")

	if err := r.Reconcile(context.Background(), nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.withdrawCalls) != 1 || f.withdrawCalls[0] != "192.168.9.9/32" {
		t.Errorf("withdrawCalls=%v, want [192.168.9.9/32]", f.withdrawCalls)
	}
}

func TestInternalReconcile_DesiredAdvertisementNotWithdrawn(t *testing.T) {
	f := newFakeServer()
	f.advertised = []bgp.Route{{Prefix: "192.168.1.10/32"}}
	r := newInternalReconciler(f, "node1")

	ov := mkOverlay("ov", "65000:1", nil, nil,
		[]v1alpha1.EdgeStatus{{NodeName: "node1", VirtualIP: "192.168.1.10", Ready: true}},
	)

	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.withdrawCalls) != 0 {
		t.Errorf("withdrawCalls=%v; desired advertisement must not be withdrawn", f.withdrawCalls)
	}
}

func TestInternalReconcile_ExistingAdvertisementNotReAdvertised(t *testing.T) {
	f := newFakeServer()
	f.advertised = []bgp.Route{{Prefix: "192.168.1.10/32"}}
	r := newInternalReconciler(f, "node1")

	ov := mkOverlay("ov", "65000:1", nil, nil,
		[]v1alpha1.EdgeStatus{{NodeName: "node1", VirtualIP: "192.168.1.10", Ready: true}},
	)

	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.advertiseCalls) != 0 {
		t.Errorf("advertiseCalls=%d; already-advertised prefix must not be re-advertised", len(f.advertiseCalls))
	}
}

// ── error propagation ─────────────────────────────────────────────────────────

func TestInternalReconcile_ListNeighborsError(t *testing.T) {
	listErr := errors.New("gobgp unavailable")
	f := newFakeServer()
	f.listNeighborsErr = listErr
	r := newInternalReconciler(f, "node1")

	err := r.Reconcile(context.Background(), nil)
	if !errors.Is(err, listErr) {
		t.Errorf("err=%v, want to wrap %v", err, listErr)
	}
}

func TestInternalReconcile_ListPoliciesError(t *testing.T) {
	listErr := errors.New("policy error")
	f := newFakeServer()
	f.listPoliciesErr = listErr
	r := newInternalReconciler(f, "node1")

	err := r.Reconcile(context.Background(), nil)
	if !errors.Is(err, listErr) {
		t.Errorf("err=%v, want to wrap %v", err, listErr)
	}
}

func TestInternalReconcile_ListAdvertisedError(t *testing.T) {
	listErr := errors.New("rib error")
	f := newFakeServer()
	f.listAdvertisedErr = listErr
	r := newInternalReconciler(f, "node1")

	err := r.Reconcile(context.Background(), nil)
	if !errors.Is(err, listErr) {
		t.Errorf("err=%v, want to wrap %v", err, listErr)
	}
}

func TestInternalReconcile_AddNeighborError(t *testing.T) {
	addErr := errors.New("neighbor create failed")
	f := newFakeServer()
	f.addNeighborErr = addErr
	r := newInternalReconciler(f, "node1")

	ov := mkOverlay("ov", "65000:1", nil,
		[]v1alpha1.Gateway{{NodeIP: "10.0.0.1"}},
		nil)

	err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov})
	if !errors.Is(err, addErr) {
		t.Errorf("err=%v, want to wrap %v", err, addErr)
	}
}

func TestInternalReconcile_AdvertiseError(t *testing.T) {
	advErr := errors.New("rib full")
	f := newFakeServer()
	f.advertiseErr = advErr
	r := newInternalReconciler(f, "node1")

	ov := mkOverlay("ov", "65000:1", nil, nil,
		[]v1alpha1.EdgeStatus{{NodeName: "node1", VirtualIP: "192.168.1.1", Ready: true}},
	)

	err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov})
	if !errors.Is(err, advErr) {
		t.Errorf("err=%v, want to wrap %v", err, advErr)
	}
}

func TestInternalReconcile_AdvertiseErrorDoesNotBlockOtherEdges(t *testing.T) {
	advErr := errors.New("rib full")
	f := newFakeServer()
	f.advertiseErr = advErr
	r := newInternalReconciler(f, "node1")

	ov := mkOverlay("ov", "65000:1", nil, nil,
		[]v1alpha1.EdgeStatus{
			{NodeName: "node1", VirtualIP: "192.168.1.1", Ready: true},
			{NodeName: "node1", VirtualIP: "192.168.1.2", Ready: true},
		},
	)

	err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if len(f.advertiseCalls) != 2 {
		t.Errorf("advertiseCalls=%d; both edges must be attempted despite errors", len(f.advertiseCalls))
	}
}
