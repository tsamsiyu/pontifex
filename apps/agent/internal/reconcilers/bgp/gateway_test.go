package bgp

import (
	"context"
	"errors"
	"testing"

	"go.uber.org/zap"

	v1alpha1 "github.com/tsamsiyu/pontifex/api/v1alpha1"
	"github.com/tsamsiyu/pontifex/apps/agent/internal/libs/bgp"
)

func newGatewayReconciler(f *fakeServer, isSecondary bool) *GatewayReconciler {
	return NewGatewayReconciler(f, 65000, "10.0.0.1", isSecondary, zap.NewNop())
}

// ── peerAddress ───────────────────────────────────────────────────────────────

func TestPeerAddress_HostPort(t *testing.T) {
	got, err := peerAddress("1.2.3.4:179")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "1.2.3.4" {
		t.Errorf("got %q, want 1.2.3.4", got)
	}
}

func TestPeerAddress_BareIP(t *testing.T) {
	got, err := peerAddress("1.2.3.4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "1.2.3.4" {
		t.Errorf("got %q, want 1.2.3.4", got)
	}
}

func TestPeerAddress_Invalid(t *testing.T) {
	_, err := peerAddress("not-valid")
	if err == nil {
		t.Fatal("expected error for invalid endpoint, got nil")
	}
}

// ── server lifecycle ──────────────────────────────────────────────────────────

func TestGatewayReconcile_StartCalledOnFirstReconcile(t *testing.T) {
	f := newFakeServer()
	r := newGatewayReconciler(f, false)
	if err := r.Reconcile(context.Background(), nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.startCalls != 1 {
		t.Errorf("startCalls=%d, want 1", f.startCalls)
	}
}

func TestGatewayReconcile_StartNotCalledAgain(t *testing.T) {
	f := newFakeServer()
	r := newGatewayReconciler(f, false)
	for i := 0; i < 3; i++ {
		if err := r.Reconcile(context.Background(), nil); err != nil {
			t.Fatalf("reconcile %d: %v", i, err)
		}
	}
	if f.startCalls != 1 {
		t.Errorf("startCalls=%d after 3 reconciles, want 1", f.startCalls)
	}
}

func TestGatewayReconcile_StartError(t *testing.T) {
	startErr := errors.New("daemon not running")
	f := newFakeServer()
	f.startErr = startErr
	r := newGatewayReconciler(f, false)

	err := r.Reconcile(context.Background(), nil)
	if !errors.Is(err, startErr) {
		t.Errorf("err=%v, want to wrap %v", err, startErr)
	}
	if len(f.addNeighborCalls) != 0 || len(f.addPolicyCalls) != 0 {
		t.Errorf("unexpected calls after start error")
	}
}

// ── no overlays ───────────────────────────────────────────────────────────────

func TestGatewayReconcile_NoOverlays(t *testing.T) {
	f := newFakeServer()
	r := newGatewayReconciler(f, false)
	if err := r.Reconcile(context.Background(), nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.addNeighborCalls) != 0 || len(f.addPolicyCalls) != 0 {
		t.Errorf("expected no calls; addNeighbor=%d addPolicy=%d",
			len(f.addNeighborCalls), len(f.addPolicyCalls))
	}
}

// ── eBGP neighbor creation ────────────────────────────────────────────────────

func TestGatewayReconcile_EBGPNeighborCreated(t *testing.T) {
	f := newFakeServer()
	r := newGatewayReconciler(f, false)

	ov := mkOverlay("test", "65000:1",
		[]v1alpha1.Peer{{Name: "peer1", Endpoint: "1.2.3.4:179", ASN: 65001}},
		nil, nil)

	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	n, ok := hasNeighbor(f.addNeighborCalls, "1.2.3.4")
	if !ok {
		t.Fatalf("neighbor 1.2.3.4 not added; calls=%+v", f.addNeighborCalls)
	}
	if n.ASN != 65001 {
		t.Errorf("ASN=%d, want 65001", n.ASN)
	}
	if n.IsIBGP {
		t.Error("IsIBGP=true, want false for eBGP peer")
	}
	if n.Name != "pntfx-peer-test-peer1" {
		t.Errorf("Name=%q, want pntfx-peer-test-peer1", n.Name)
	}
}

func TestGatewayReconcile_EBGPNeighborHasPolicies(t *testing.T) {
	f := newFakeServer()
	r := newGatewayReconciler(f, false)

	ov := mkOverlay("ov1", "65000:1",
		[]v1alpha1.Peer{{Name: "p", Endpoint: "5.5.5.5:179", ASN: 65002}},
		nil, nil)

	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	n, ok := hasNeighbor(f.addNeighborCalls, "5.5.5.5")
	if !ok {
		t.Fatal("neighbor 5.5.5.5 not added")
	}
	if !containsStr(n.Policies, "pntfx-import-ov1") {
		t.Errorf("Policies=%v, missing pntfx-import-ov1", n.Policies)
	}
	if !containsStr(n.Policies, "pntfx-export-ov1") {
		t.Errorf("Policies=%v, missing pntfx-export-ov1", n.Policies)
	}
}

// ── iBGP neighbor creation ────────────────────────────────────────────────────

func TestGatewayReconcile_IBGPNeighborCreatedFromEdge(t *testing.T) {
	f := newFakeServer()
	r := newGatewayReconciler(f, false)

	ov := mkOverlay("ov", "65000:1", nil, nil,
		[]v1alpha1.EdgeStatus{{NodeIP: "10.0.1.5", NodeName: "node1", VirtualIP: "192.168.1.1", Ready: true}},
	)

	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	n, ok := hasNeighbor(f.addNeighborCalls, "10.0.1.5")
	if !ok {
		t.Fatalf("iBGP neighbor 10.0.1.5 not added")
	}
	if !n.IsIBGP {
		t.Error("IsIBGP=false, want true for internal node")
	}
	if n.ASN != 65000 {
		t.Errorf("ASN=%d, want 65000 (local ASN)", n.ASN)
	}
	if n.Name != "pntfx-internal-10.0.1.5" {
		t.Errorf("Name=%q, want pntfx-internal-10.0.1.5", n.Name)
	}
}

func TestGatewayReconcile_IBGPNotReadyEdgeSkipped(t *testing.T) {
	f := newFakeServer()
	r := newGatewayReconciler(f, false)

	ov := mkOverlay("ov", "65000:1", nil, nil,
		[]v1alpha1.EdgeStatus{{NodeIP: "10.0.1.5", Ready: false}},
	)

	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := hasNeighbor(f.addNeighborCalls, "10.0.1.5"); ok {
		t.Error("neighbor added for non-ready edge, want none")
	}
}

func TestGatewayReconcile_IBGPDeduplicatedAcrossOverlays(t *testing.T) {
	f := newFakeServer()
	r := newGatewayReconciler(f, false)

	edge := v1alpha1.EdgeStatus{NodeIP: "10.0.1.5", NodeName: "node1", VirtualIP: "192.168.1.1", Ready: true}
	ov1 := mkOverlay("ov1", "65000:1", nil, nil, []v1alpha1.EdgeStatus{edge})
	ov2 := mkOverlay("ov2", "65000:2", nil, nil, []v1alpha1.EdgeStatus{edge})

	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov1, ov2}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	count := 0
	for _, n := range f.addNeighborCalls {
		if n.Address == "10.0.1.5" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("neighbor 10.0.1.5 added %d times, want 1", count)
	}
}

// ── duplicate eBGP address ────────────────────────────────────────────────────

func TestGatewayReconcile_DuplicatePeerAddressKeptOnce(t *testing.T) {
	f := newFakeServer()
	r := newGatewayReconciler(f, false)

	peer := v1alpha1.Peer{Name: "p", Endpoint: "9.9.9.9:179", ASN: 65001}
	ov1 := mkOverlay("ov1", "65000:1", []v1alpha1.Peer{peer}, nil, nil)
	ov2 := mkOverlay("ov2", "65000:2", []v1alpha1.Peer{peer}, nil, nil)

	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov1, ov2}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	count := 0
	for _, n := range f.addNeighborCalls {
		if n.Address == "9.9.9.9" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("neighbor 9.9.9.9 added %d times, want 1", count)
	}
}

// ── policies ──────────────────────────────────────────────────────────────────

func TestGatewayReconcile_PoliciesCreatedForOverlay(t *testing.T) {
	f := newFakeServer()
	r := newGatewayReconciler(f, false)

	ov := mkOverlay("myov", "65000:42", nil, nil, nil)
	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	imp, ok := hasPolicy(f.addPolicyCalls, "pntfx-import-myov")
	if !ok {
		t.Fatal("import policy not added")
	}
	if imp.Direction != bgp.PolicyImport {
		t.Errorf("import policy Direction=%v, want PolicyImport", imp.Direction)
	}
	if imp.Action != "community-set:65000:42" {
		t.Errorf("import Action=%q, want community-set:65000:42", imp.Action)
	}

	exp, ok := hasPolicy(f.addPolicyCalls, "pntfx-export-myov")
	if !ok {
		t.Fatal("export policy not added")
	}
	if exp.Direction != bgp.PolicyExport {
		t.Errorf("export policy Direction=%v, want PolicyExport", exp.Direction)
	}
	if exp.Match != "community:65000:42" {
		t.Errorf("export Match=%q, want community:65000:42", exp.Match)
	}
}

func TestGatewayReconcile_SecondaryPrependPolicyAdded(t *testing.T) {
	f := newFakeServer()
	r := newGatewayReconciler(f, true) // isSecondary=true

	ov := mkOverlay("ov", "65000:1", nil, nil, nil)
	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := hasPolicy(f.addPolicyCalls, "pntfx-prepend-ov"); !ok {
		t.Error("prepend policy not added for secondary gateway")
	}
}

func TestGatewayReconcile_SecondaryPrependAbsentWhenPrimary(t *testing.T) {
	f := newFakeServer()
	r := newGatewayReconciler(f, false) // isSecondary=false

	ov := mkOverlay("ov", "65000:1", nil, nil, nil)
	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := hasPolicy(f.addPolicyCalls, "pntfx-prepend-ov"); ok {
		t.Error("prepend policy added for primary gateway, want none")
	}
}

func TestGatewayReconcile_SecondaryPrependInNeighborPolicies(t *testing.T) {
	f := newFakeServer()
	r := newGatewayReconciler(f, true)

	ov := mkOverlay("ov", "65000:1",
		[]v1alpha1.Peer{{Name: "p", Endpoint: "2.2.2.2:179", ASN: 65001}},
		nil, nil)

	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	n, ok := hasNeighbor(f.addNeighborCalls, "2.2.2.2")
	if !ok {
		t.Fatal("neighbor not added")
	}
	if !containsStr(n.Policies, "pntfx-prepend-ov") {
		t.Errorf("Policies=%v, missing pntfx-prepend-ov for secondary", n.Policies)
	}
}

// ── empty community skipped ───────────────────────────────────────────────────

func TestGatewayReconcile_OverlayWithEmptyCommunitySkipped(t *testing.T) {
	f := newFakeServer()
	r := newGatewayReconciler(f, false)

	ov := mkOverlay("ov", "",
		[]v1alpha1.Peer{{Name: "p", Endpoint: "3.3.3.3:179", ASN: 65001}},
		nil, nil)

	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.addNeighborCalls) != 0 || len(f.addPolicyCalls) != 0 {
		t.Errorf("expected no calls for empty-community overlay; addNeighbor=%d addPolicy=%d",
			len(f.addNeighborCalls), len(f.addPolicyCalls))
	}
}

// ── orphan removal ────────────────────────────────────────────────────────────

func TestGatewayReconcile_OrphanNeighborRemoved(t *testing.T) {
	f := newFakeServer()
	f.neighbors = []bgp.Neighbor{{Address: "8.8.8.8"}}
	r := newGatewayReconciler(f, false)

	if err := r.Reconcile(context.Background(), nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.removeNeighborCalls) != 1 || f.removeNeighborCalls[0] != "8.8.8.8" {
		t.Errorf("removeNeighborCalls=%v, want [8.8.8.8]", f.removeNeighborCalls)
	}
}

func TestGatewayReconcile_OrphanPolicyRemoved(t *testing.T) {
	f := newFakeServer()
	f.policies = []bgp.Policy{{Name: "pntfx-import-stale"}}
	r := newGatewayReconciler(f, false)

	if err := r.Reconcile(context.Background(), nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.removePolicyCalls) != 1 || f.removePolicyCalls[0] != "pntfx-import-stale" {
		t.Errorf("removePolicyCalls=%v, want [pntfx-import-stale]", f.removePolicyCalls)
	}
}

func TestGatewayReconcile_DesiredNeighborNotRemoved(t *testing.T) {
	f := newFakeServer()
	f.neighbors = []bgp.Neighbor{{Address: "1.2.3.4"}}
	r := newGatewayReconciler(f, false)

	ov := mkOverlay("ov", "65000:1",
		[]v1alpha1.Peer{{Name: "p", Endpoint: "1.2.3.4:179", ASN: 65001}},
		nil, nil)

	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.removeNeighborCalls) != 0 {
		t.Errorf("removeNeighborCalls=%v; desired neighbor must not be removed", f.removeNeighborCalls)
	}
}

// ── error propagation ─────────────────────────────────────────────────────────

func TestGatewayReconcile_ListNeighborsError(t *testing.T) {
	listErr := errors.New("grpc down")
	f := newFakeServer()
	f.listNeighborsErr = listErr
	r := newGatewayReconciler(f, false)

	err := r.Reconcile(context.Background(), nil)
	if !errors.Is(err, listErr) {
		t.Errorf("err=%v, want to wrap %v", err, listErr)
	}
}

func TestGatewayReconcile_ListPoliciesError(t *testing.T) {
	listErr := errors.New("policy store error")
	f := newFakeServer()
	f.listPoliciesErr = listErr
	r := newGatewayReconciler(f, false)

	err := r.Reconcile(context.Background(), nil)
	if !errors.Is(err, listErr) {
		t.Errorf("err=%v, want to wrap %v", err, listErr)
	}
}

func TestGatewayReconcile_AddNeighborError(t *testing.T) {
	addErr := errors.New("neighbor create failed")
	f := newFakeServer()
	f.addNeighborErr = addErr
	r := newGatewayReconciler(f, false)

	ov := mkOverlay("ov", "65000:1",
		[]v1alpha1.Peer{{Name: "p", Endpoint: "4.4.4.4:179", ASN: 65001}},
		nil, nil)

	err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov})
	if !errors.Is(err, addErr) {
		t.Errorf("err=%v, want to wrap %v", err, addErr)
	}
}

// ── multiple overlays ─────────────────────────────────────────────────────────

func TestGatewayReconcile_MultipleOverlays(t *testing.T) {
	f := newFakeServer()
	r := newGatewayReconciler(f, false)

	ov1 := mkOverlay("ov1", "65000:1",
		[]v1alpha1.Peer{{Name: "p1", Endpoint: "1.1.1.1:179", ASN: 65001}},
		nil, nil)
	ov2 := mkOverlay("ov2", "65000:2",
		[]v1alpha1.Peer{{Name: "p2", Endpoint: "2.2.2.2:179", ASN: 65002}},
		nil, nil)

	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov1, ov2}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.addNeighborCalls) != 2 {
		t.Errorf("addNeighborCalls=%d, want 2", len(f.addNeighborCalls))
	}
	// Two import + two export policies.
	if len(f.addPolicyCalls) != 4 {
		t.Errorf("addPolicyCalls=%d, want 4 (2 import + 2 export)", len(f.addPolicyCalls))
	}
}

// ── existing entries not re-added ─────────────────────────────────────────────

func TestGatewayReconcile_ExistingNeighborNotAdded(t *testing.T) {
	f := newFakeServer()
	f.neighbors = []bgp.Neighbor{{Address: "1.2.3.4"}}
	r := newGatewayReconciler(f, false)

	ov := mkOverlay("ov", "65000:1",
		[]v1alpha1.Peer{{Name: "p", Endpoint: "1.2.3.4:179", ASN: 65001}},
		nil, nil)

	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.addNeighborCalls) != 0 {
		t.Errorf("addNeighborCalls=%d; existing neighbor must not be re-added", len(f.addNeighborCalls))
	}
}

func TestGatewayReconcile_ExistingPolicyNotAdded(t *testing.T) {
	f := newFakeServer()
	f.policies = []bgp.Policy{{Name: "pntfx-import-ov"}}
	r := newGatewayReconciler(f, false)

	ov := mkOverlay("ov", "65000:1", nil, nil, nil)
	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := hasPolicy(f.addPolicyCalls, "pntfx-import-ov"); ok {
		t.Error("pntfx-import-ov re-added; must be skipped when already present")
	}
}

