package routes

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"go.uber.org/zap"

	v1alpha1 "github.com/tsamsiyu/pontifex/api/v1alpha1"
	"github.com/tsamsiyu/pontifex/apps/agent/internal/libs/bgp"
	"github.com/tsamsiyu/pontifex/apps/agent/internal/libs/firewall"
	"github.com/tsamsiyu/pontifex/apps/agent/internal/libs/routes"
)

// ── fakeRoutes ────────────────────────────────────────────────────────────────

type syncCall struct {
	tableID uint32
	desired []routes.Route
}

type fakeRoutes struct {
	vrfs  []routes.VRF
	addrs map[string][]routes.Addr // key = vrfName
	rules []routes.Rule

	listVRFsErr   error
	listAddrsErr  error
	listRulesErr  error
	syncRoutesErr error
	ensureVRFErr  error
	removeVRFErr  error
	ensureAddrErr error
	removeAddrErr error
	ensureRuleErr error
	removeRuleErr error

	ensureVRFCalls  []routes.VRF
	removeVRFCalls  []string
	ensureAddrCalls []routes.Addr
	removeAddrCalls []routes.Addr
	ensureRuleCalls []routes.Rule
	removeRuleCalls []routes.Rule
	syncRoutesCalls []syncCall
}

var _ routes.Routes = (*fakeRoutes)(nil)

func newFakeRoutes() *fakeRoutes {
	return &fakeRoutes{addrs: make(map[string][]routes.Addr)}
}

func (f *fakeRoutes) EnsureVRF(_ context.Context, name string, tableID uint32) error {
	f.ensureVRFCalls = append(f.ensureVRFCalls, routes.VRF{Name: name, TableID: tableID})
	return f.ensureVRFErr
}

func (f *fakeRoutes) RemoveVRF(_ context.Context, name string) error {
	f.removeVRFCalls = append(f.removeVRFCalls, name)
	return f.removeVRFErr
}

func (f *fakeRoutes) ListVRFs(_ context.Context) ([]routes.VRF, error) {
	return f.vrfs, f.listVRFsErr
}

func (f *fakeRoutes) EnsureAddr(_ context.Context, vrfName, ip string) error {
	f.ensureAddrCalls = append(f.ensureAddrCalls, routes.Addr{VRFName: vrfName, IP: ip})
	return f.ensureAddrErr
}

func (f *fakeRoutes) RemoveAddr(_ context.Context, vrfName, ip string) error {
	f.removeAddrCalls = append(f.removeAddrCalls, routes.Addr{VRFName: vrfName, IP: ip})
	return f.removeAddrErr
}

func (f *fakeRoutes) ListAddrs(_ context.Context, vrfName string) ([]routes.Addr, error) {
	return f.addrs[vrfName], f.listAddrsErr
}

func (f *fakeRoutes) EnsureRule(_ context.Context, r routes.Rule) error {
	f.ensureRuleCalls = append(f.ensureRuleCalls, r)
	return f.ensureRuleErr
}

func (f *fakeRoutes) RemoveRule(_ context.Context, r routes.Rule) error {
	f.removeRuleCalls = append(f.removeRuleCalls, r)
	return f.removeRuleErr
}

func (f *fakeRoutes) ListRules(_ context.Context) ([]routes.Rule, error) {
	return f.rules, f.listRulesErr
}

func (f *fakeRoutes) AddRoute(_ context.Context, tableID uint32, dst, gw, dev string) error {
	return nil
}

func (f *fakeRoutes) RemoveRoute(_ context.Context, tableID uint32, dst string) error {
	return nil
}

func (f *fakeRoutes) SyncRoutes(_ context.Context, tableID uint32, desired []routes.Route) error {
	f.syncRoutesCalls = append(f.syncRoutesCalls, syncCall{tableID: tableID, desired: desired})
	return f.syncRoutesErr
}

func (f *fakeRoutes) ListRoutes(_ context.Context, tableID uint32) ([]routes.Route, error) {
	return nil, nil
}

// ── fakeFirewall ──────────────────────────────────────────────────────────────

type removeBridgeCall struct {
	overlayName string
	virtualIP   string
}

type fakeFirewall struct {
	bridges []firewall.Bridge

	listErr   error
	ensureErr error
	removeErr error

	ensureCalls []firewall.Bridge
	removeCalls []removeBridgeCall
}

var _ firewall.Firewall = (*fakeFirewall)(nil)

func newFakeFirewall() *fakeFirewall {
	return &fakeFirewall{}
}

func (f *fakeFirewall) EnsureBridge(_ context.Context, overlayName, virtualIP, podIP string) error {
	f.ensureCalls = append(f.ensureCalls, firewall.Bridge{
		OverlayName: overlayName,
		VirtualIP:   virtualIP,
		PodIP:       podIP,
	})
	return f.ensureErr
}

func (f *fakeFirewall) RemoveBridge(_ context.Context, overlayName, virtualIP string) error {
	f.removeCalls = append(f.removeCalls, removeBridgeCall{overlayName: overlayName, virtualIP: virtualIP})
	return f.removeErr
}

func (f *fakeFirewall) ListBridges(_ context.Context) ([]firewall.Bridge, error) {
	return f.bridges, f.listErr
}

// ── test constructors ─────────────────────────────────────────────────────────

func newGatewayReconcilerForTest(f *fakeRoutes) *GatewayReconciler {
	return NewGatewayReconciler(f, nopLogger())
}

func newInternalReconcilerForTest(rt *fakeRoutes, fw *fakeFirewall, nodeName string) *InternalReconciler {
	return NewInternalReconciler(rt, fw, nodeName, nopLogger())
}

func nopLogger() *zap.Logger {
	return zap.NewNop()
}

// ── BGP event helpers ─────────────────────────────────────────────────────────

func addedEvent(prefix, nextHop string, communities ...string) bgp.RouteEvent {
	return bgp.RouteEvent{
		Type: bgp.RouteAdded,
		Route: bgp.Route{
			Prefix:      prefix,
			NextHop:     nextHop,
			Communities: communities,
		},
	}
}

func withdrawnEvent(prefix string) bgp.RouteEvent {
	return bgp.RouteEvent{
		Type:  bgp.RouteWithdrawn,
		Route: bgp.Route{Prefix: prefix},
	}
}

// ── overlay builder ───────────────────────────────────────────────────────────

// mkOverlay constructs a NetworkOverlay for tests.
func mkOverlay(name, community, virtualCIDR string, gateways []v1alpha1.Gateway, edges []v1alpha1.EdgeStatus) v1alpha1.NetworkOverlay {
	return v1alpha1.NetworkOverlay{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: v1alpha1.NetworkOverlaySpec{
			VirtualCIDR: virtualCIDR,
		},
		Status: v1alpha1.NetworkOverlayStatus{
			Community: community,
			Gateways:  gateways,
			Edges:     edges,
		},
	}
}

// ── assertion helpers ─────────────────────────────────────────────────────────

func hasSyncCall(calls []syncCall, tableID uint32) (syncCall, bool) {
	for _, c := range calls {
		if c.tableID == tableID {
			return c, true
		}
	}
	return syncCall{}, false
}

func syncDesiredContains(desired []routes.Route, dst string) bool {
	for _, r := range desired {
		if r.Dst == dst {
			return true
		}
	}
	return false
}

func hasEnsureVRF(calls []routes.VRF, name string) (routes.VRF, bool) {
	for _, v := range calls {
		if v.Name == name {
			return v, true
		}
	}
	return routes.VRF{}, false
}

func hasEnsureAddr(calls []routes.Addr, vrfName, ip string) bool {
	for _, a := range calls {
		if a.VRFName == vrfName && a.IP == ip {
			return true
		}
	}
	return false
}

func hasEnsureRule(calls []routes.Rule, tableID uint32) (routes.Rule, bool) {
	for _, r := range calls {
		if r.TableID == tableID {
			return r, true
		}
	}
	return routes.Rule{}, false
}

func hasEnsureBridge(calls []firewall.Bridge, overlayName, vip string) (firewall.Bridge, bool) {
	for _, b := range calls {
		if b.OverlayName == overlayName && b.VirtualIP == vip {
			return b, true
		}
	}
	return firewall.Bridge{}, false
}
