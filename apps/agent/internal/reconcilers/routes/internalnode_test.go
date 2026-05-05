package routes

import (
	"context"
	"errors"
	"testing"

	"go.uber.org/zap"

	v1alpha1 "github.com/tsamsiyu/pontifex/api/v1alpha1"
	"github.com/tsamsiyu/pontifex/apps/agent/internal/libs/bgp"
	"github.com/tsamsiyu/pontifex/apps/agent/internal/libs/firewall"
	"github.com/tsamsiyu/pontifex/apps/agent/internal/libs/routes"
)

func newInternalReconciler(rt *fakeRoutes, fw *fakeFirewall, events <-chan bgp.RouteEvent, nodeName string) *InternalReconciler {
	return NewInternalReconciler(rt, fw, events, nodeName, zap.NewNop())
}

// ── vrfTableID helper ─────────────────────────────────────────────────────────

func TestVRFTableID_Valid(t *testing.T) {
	tid, err := vrfTableID("65000:3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tid != 10003 {
		t.Errorf("tableID=%d, want 10003", tid)
	}
}

func TestVRFTableID_InvalidFormat(t *testing.T) {
	_, err := vrfTableID("65000")
	if err == nil {
		t.Fatal("expected error for community without colon, got nil")
	}
}

func TestVRFTableID_InvalidNumber(t *testing.T) {
	_, err := vrfTableID("65000:notanumber")
	if err == nil {
		t.Fatal("expected error for non-numeric community number, got nil")
	}
}

// ── no-op ─────────────────────────────────────────────────────────────────────

func TestInternal_NoOverlays(t *testing.T) {
	rt := newFakeRoutes()
	fw := newFakeFirewall()
	r := newInternalReconciler(rt, fw, mkBGPEvents(), "node1")

	if err := r.Reconcile(context.Background(), nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rt.ensureVRFCalls) != 0 || len(rt.ensureRuleCalls) != 0 ||
		len(rt.ensureAddrCalls) != 0 || len(fw.ensureCalls) != 0 {
		t.Error("expected no calls for empty overlay list")
	}
}

// ── VRFs ──────────────────────────────────────────────────────────────────────

func TestInternal_VRFEnsuredForOverlayWithCommunity(t *testing.T) {
	rt := newFakeRoutes()
	fw := newFakeFirewall()
	r := newInternalReconciler(rt, fw, mkBGPEvents(), "node1")

	ov := mkOverlay("myov", "65000:1", "", nil, nil)
	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	v, ok := hasEnsureVRF(rt.ensureVRFCalls, "pntfx-myov")
	if !ok {
		t.Fatal("EnsureVRF not called for pntfx-myov")
	}
	if v.TableID != 10001 {
		t.Errorf("tableID=%d, want 10001", v.TableID)
	}
}

func TestInternal_VRFNotEnsuredForNoCommunity(t *testing.T) {
	rt := newFakeRoutes()
	fw := newFakeFirewall()
	r := newInternalReconciler(rt, fw, mkBGPEvents(), "node1")

	ov := mkOverlay("ov", "", "", nil, nil)
	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rt.ensureVRFCalls) != 0 {
		t.Errorf("ensureVRFCalls=%d; overlay without community must not create VRF", len(rt.ensureVRFCalls))
	}
}

func TestInternal_VRFNameDerivedCorrectly(t *testing.T) {
	rt := newFakeRoutes()
	fw := newFakeFirewall()
	r := newInternalReconciler(rt, fw, mkBGPEvents(), "node1")

	ov := mkOverlay("myov", "65000:2", "", nil, nil)
	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := hasEnsureVRF(rt.ensureVRFCalls, "pntfx-myov"); !ok {
		t.Errorf("ensureVRFCalls=%v; want pntfx-myov", rt.ensureVRFCalls)
	}
}

func TestInternal_OrphanVRFRemoved(t *testing.T) {
	rt := newFakeRoutes()
	rt.vrfs = []routes.VRF{{Name: "pntfx-stale", TableID: 10099}}
	fw := newFakeFirewall()
	r := newInternalReconciler(rt, fw, mkBGPEvents(), "node1")

	if err := r.Reconcile(context.Background(), nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rt.removeVRFCalls) != 1 || rt.removeVRFCalls[0] != "pntfx-stale" {
		t.Errorf("removeVRFCalls=%v, want [pntfx-stale]", rt.removeVRFCalls)
	}
}

func TestInternal_DesiredVRFNotRemoved(t *testing.T) {
	rt := newFakeRoutes()
	rt.vrfs = []routes.VRF{{Name: "pntfx-ov", TableID: 10001}}
	fw := newFakeFirewall()
	r := newInternalReconciler(rt, fw, mkBGPEvents(), "node1")

	ov := mkOverlay("ov", "65000:1", "", nil, nil)
	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rt.removeVRFCalls) != 0 {
		t.Errorf("removeVRFCalls=%v; desired VRF must not be removed", rt.removeVRFCalls)
	}
}

func TestInternal_ListVRFsError(t *testing.T) {
	listErr := errors.New("netlink down")
	rt := newFakeRoutes()
	rt.listVRFsErr = listErr
	fw := newFakeFirewall()
	r := newInternalReconciler(rt, fw, mkBGPEvents(), "node1")

	err := r.Reconcile(context.Background(), nil)
	if !errors.Is(err, listErr) {
		t.Errorf("err=%v, want to wrap %v", err, listErr)
	}
}

// ── ip rules ──────────────────────────────────────────────────────────────────

func TestInternal_RuleEnsuredForOverlayWithVirtualCIDR(t *testing.T) {
	rt := newFakeRoutes()
	fw := newFakeFirewall()
	r := newInternalReconciler(rt, fw, mkBGPEvents(), "node1")

	ov := mkOverlay("ov", "65000:1", "192.168.0.0/24", nil, nil)
	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rule, ok := hasEnsureRule(rt.ensureRuleCalls, 10001)
	if !ok {
		t.Fatal("EnsureRule not called for tableID 10001")
	}
	if rule.To != "192.168.0.0/24" {
		t.Errorf("rule.To=%q, want 192.168.0.0/24", rule.To)
	}
}

func TestInternal_RuleNotEnsuredForEmptyVirtualCIDR(t *testing.T) {
	rt := newFakeRoutes()
	fw := newFakeFirewall()
	r := newInternalReconciler(rt, fw, mkBGPEvents(), "node1")

	ov := mkOverlay("ov", "65000:1", "", nil, nil) // empty VirtualCIDR
	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rt.ensureRuleCalls) != 0 {
		t.Errorf("ensureRuleCalls=%d; must not create rule for empty VirtualCIDR", len(rt.ensureRuleCalls))
	}
}

func TestInternal_OrphanRuleRemoved(t *testing.T) {
	rt := newFakeRoutes()
	rt.rules = []routes.Rule{{To: "10.99.0.0/16", TableID: 10099}}
	fw := newFakeFirewall()
	r := newInternalReconciler(rt, fw, mkBGPEvents(), "node1")

	if err := r.Reconcile(context.Background(), nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rt.removeRuleCalls) != 1 || rt.removeRuleCalls[0].TableID != 10099 {
		t.Errorf("removeRuleCalls=%v, want rule with tableID 10099", rt.removeRuleCalls)
	}
}

func TestInternal_ListRulesError(t *testing.T) {
	listErr := errors.New("rule list failed")
	rt := newFakeRoutes()
	rt.listRulesErr = listErr
	fw := newFakeFirewall()
	r := newInternalReconciler(rt, fw, mkBGPEvents(), "node1")

	err := r.Reconcile(context.Background(), nil)
	if !errors.Is(err, listErr) {
		t.Errorf("err=%v, want to wrap %v", err, listErr)
	}
}

// ── addresses ─────────────────────────────────────────────────────────────────

func TestInternal_AddrEnsuredForReadyEdgeOnThisNode(t *testing.T) {
	rt := newFakeRoutes()
	fw := newFakeFirewall()
	r := newInternalReconciler(rt, fw, mkBGPEvents(), "node1")

	ov := mkOverlay("ov", "65000:1", "", nil,
		[]v1alpha1.EdgeStatus{{NodeName: "node1", VirtualIP: "192.168.1.10", PodIP: "10.0.0.5", Ready: true}},
	)
	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !hasEnsureAddr(rt.ensureAddrCalls, "pntfx-ov", "192.168.1.10") {
		t.Errorf("ensureAddrCalls=%v; missing pntfx-ov/192.168.1.10", rt.ensureAddrCalls)
	}
}

func TestInternal_AddrNotEnsuredForOtherNode(t *testing.T) {
	rt := newFakeRoutes()
	fw := newFakeFirewall()
	r := newInternalReconciler(rt, fw, mkBGPEvents(), "node1")

	ov := mkOverlay("ov", "65000:1", "", nil,
		[]v1alpha1.EdgeStatus{{NodeName: "node2", VirtualIP: "192.168.1.10", Ready: true}},
	)
	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rt.ensureAddrCalls) != 0 {
		t.Errorf("ensureAddrCalls=%d; edge on other node must not install addr", len(rt.ensureAddrCalls))
	}
}

func TestInternal_AddrNotEnsuredForNotReadyEdge(t *testing.T) {
	rt := newFakeRoutes()
	fw := newFakeFirewall()
	r := newInternalReconciler(rt, fw, mkBGPEvents(), "node1")

	ov := mkOverlay("ov", "65000:1", "", nil,
		[]v1alpha1.EdgeStatus{{NodeName: "node1", VirtualIP: "192.168.1.10", Ready: false}},
	)
	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rt.ensureAddrCalls) != 0 {
		t.Errorf("ensureAddrCalls=%d; non-ready edge must not install addr", len(rt.ensureAddrCalls))
	}
}

func TestInternal_AddrNotEnsuredForEmptyVIP(t *testing.T) {
	rt := newFakeRoutes()
	fw := newFakeFirewall()
	r := newInternalReconciler(rt, fw, mkBGPEvents(), "node1")

	ov := mkOverlay("ov", "65000:1", "", nil,
		[]v1alpha1.EdgeStatus{{NodeName: "node1", VirtualIP: "", Ready: true}},
	)
	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rt.ensureAddrCalls) != 0 {
		t.Errorf("ensureAddrCalls=%d; empty VirtualIP must be skipped", len(rt.ensureAddrCalls))
	}
}

func TestInternal_OrphanAddrRemoved(t *testing.T) {
	rt := newFakeRoutes()
	rt.addrs["pntfx-ov"] = []routes.Addr{{VRFName: "pntfx-ov", IP: "192.168.1.99"}}
	fw := newFakeFirewall()
	r := newInternalReconciler(rt, fw, mkBGPEvents(), "node1")

	ov := mkOverlay("ov", "65000:1", "", nil, nil)
	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rt.removeAddrCalls) != 1 || rt.removeAddrCalls[0].IP != "192.168.1.99" {
		t.Errorf("removeAddrCalls=%v, want [{pntfx-ov 192.168.1.99}]", rt.removeAddrCalls)
	}
}

// ── BGP routes ────────────────────────────────────────────────────────────────

func TestInternal_BGPRouteSyncedToCorrectTable(t *testing.T) {
	rt := newFakeRoutes()
	fw := newFakeFirewall()
	ch := mkBGPEvents(addedRoute("10.10.0.0/24", "172.16.0.1", "65000:1"))
	r := newInternalReconciler(rt, fw, ch, "node1")

	ov := mkOverlay("ov", "65000:1", "", nil, nil)
	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sc, ok := hasSyncCall(rt.syncRoutesCalls, 10001)
	if !ok {
		t.Fatal("SyncRoutes not called for table 10001")
	}
	if !syncDesiredContains(sc.desired, "10.10.0.0/24") {
		t.Errorf("desired=%v, missing 10.10.0.0/24", sc.desired)
	}
}

func TestInternal_BGPRouteWithdrawnNotInSync(t *testing.T) {
	rt := newFakeRoutes()
	fw := newFakeFirewall()
	ch := mkBGPEvents(
		addedRoute("10.10.0.0/24", "172.16.0.1", "65000:1"),
		withdrawnRoute("10.10.0.0/24"),
	)
	r := newInternalReconciler(rt, fw, ch, "node1")

	ov := mkOverlay("ov", "65000:1", "", nil, nil)
	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sc, _ := hasSyncCall(rt.syncRoutesCalls, 10001)
	if syncDesiredContains(sc.desired, "10.10.0.0/24") {
		t.Error("withdrawn route must not appear in VRF table sync")
	}
}

func TestInternal_BGPRouteWithUnknownCommunityIgnored(t *testing.T) {
	rt := newFakeRoutes()
	fw := newFakeFirewall()
	// Route carries community 65000:99 but overlay only has 65000:1.
	ch := mkBGPEvents(addedRoute("10.10.0.0/24", "172.16.0.1", "65000:99"))
	r := newInternalReconciler(rt, fw, ch, "node1")

	ov := mkOverlay("ov", "65000:1", "", nil, nil)
	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sc, _ := hasSyncCall(rt.syncRoutesCalls, 10001)
	if syncDesiredContains(sc.desired, "10.10.0.0/24") {
		t.Error("route with unknown community must not appear in any VRF table")
	}
}

func TestInternal_SyncRoutesError(t *testing.T) {
	syncErr := errors.New("table write failed")
	rt := newFakeRoutes()
	rt.syncRoutesErr = syncErr
	fw := newFakeFirewall()
	r := newInternalReconciler(rt, fw, mkBGPEvents(), "node1")

	ov := mkOverlay("ov", "65000:1", "", nil, nil)
	err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov})
	if !errors.Is(err, syncErr) {
		t.Errorf("err=%v, want to wrap %v", err, syncErr)
	}
}

// ── firewall bridges ──────────────────────────────────────────────────────────

func TestInternal_BridgeEnsuredForReadyEdgeWithPodIP(t *testing.T) {
	rt := newFakeRoutes()
	fw := newFakeFirewall()
	r := newInternalReconciler(rt, fw, mkBGPEvents(), "node1")

	ov := mkOverlay("ov", "65000:1", "", nil,
		[]v1alpha1.EdgeStatus{{
			NodeName:  "node1",
			VirtualIP: "192.168.1.10",
			PodIP:     "10.0.0.5",
			Ready:     true,
		}},
	)
	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	b, ok := hasEnsureBridge(fw.ensureCalls, "ov", "192.168.1.10")
	if !ok {
		t.Fatal("EnsureBridge not called for ov/192.168.1.10")
	}
	if b.PodIP != "10.0.0.5" {
		t.Errorf("PodIP=%q, want 10.0.0.5", b.PodIP)
	}
}

func TestInternal_BridgeNotEnsuredForOtherNode(t *testing.T) {
	rt := newFakeRoutes()
	fw := newFakeFirewall()
	r := newInternalReconciler(rt, fw, mkBGPEvents(), "node1")

	ov := mkOverlay("ov", "65000:1", "", nil,
		[]v1alpha1.EdgeStatus{{NodeName: "node2", VirtualIP: "192.168.1.10", PodIP: "10.0.0.5", Ready: true}},
	)
	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fw.ensureCalls) != 0 {
		t.Errorf("ensureCalls=%d; edge on other node must not create bridge", len(fw.ensureCalls))
	}
}

func TestInternal_BridgeNotEnsuredForNotReadyEdge(t *testing.T) {
	rt := newFakeRoutes()
	fw := newFakeFirewall()
	r := newInternalReconciler(rt, fw, mkBGPEvents(), "node1")

	ov := mkOverlay("ov", "65000:1", "", nil,
		[]v1alpha1.EdgeStatus{{NodeName: "node1", VirtualIP: "192.168.1.10", PodIP: "10.0.0.5", Ready: false}},
	)
	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fw.ensureCalls) != 0 {
		t.Errorf("ensureCalls=%d; non-ready edge must not create bridge", len(fw.ensureCalls))
	}
}

func TestInternal_BridgeNotEnsuredForEmptyPodIP(t *testing.T) {
	rt := newFakeRoutes()
	fw := newFakeFirewall()
	r := newInternalReconciler(rt, fw, mkBGPEvents(), "node1")

	ov := mkOverlay("ov", "65000:1", "", nil,
		[]v1alpha1.EdgeStatus{{NodeName: "node1", VirtualIP: "192.168.1.10", PodIP: "", Ready: true}},
	)
	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fw.ensureCalls) != 0 {
		t.Errorf("ensureCalls=%d; empty PodIP must be skipped", len(fw.ensureCalls))
	}
}

func TestInternal_OrphanBridgeRemoved(t *testing.T) {
	rt := newFakeRoutes()
	fw := newFakeFirewall()
	fw.bridges = []firewall.Bridge{{OverlayName: "ov", VirtualIP: "192.168.1.99", PodIP: "10.0.0.99"}}
	r := newInternalReconciler(rt, fw, mkBGPEvents(), "node1")

	if err := r.Reconcile(context.Background(), nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fw.removeCalls) != 1 || fw.removeCalls[0].overlayName != "ov" || fw.removeCalls[0].virtualIP != "192.168.1.99" {
		t.Errorf("removeCalls=%v, want [{ov 192.168.1.99}]", fw.removeCalls)
	}
}

func TestInternal_DesiredBridgeNotRemoved(t *testing.T) {
	rt := newFakeRoutes()
	fw := newFakeFirewall()
	fw.bridges = []firewall.Bridge{{OverlayName: "ov", VirtualIP: "192.168.1.10", PodIP: "10.0.0.5"}}
	r := newInternalReconciler(rt, fw, mkBGPEvents(), "node1")

	ov := mkOverlay("ov", "65000:1", "", nil,
		[]v1alpha1.EdgeStatus{{NodeName: "node1", VirtualIP: "192.168.1.10", PodIP: "10.0.0.5", Ready: true}},
	)
	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{ov}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fw.removeCalls) != 0 {
		t.Errorf("removeCalls=%v; desired bridge must not be removed", fw.removeCalls)
	}
}

func TestInternal_ListBridgesError(t *testing.T) {
	listErr := errors.New("iptables unavailable")
	rt := newFakeRoutes()
	fw := newFakeFirewall()
	fw.listErr = listErr
	r := newInternalReconciler(rt, fw, mkBGPEvents(), "node1")

	err := r.Reconcile(context.Background(), nil)
	if !errors.Is(err, listErr) {
		t.Errorf("err=%v, want to wrap %v", err, listErr)
	}
}
