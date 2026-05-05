package bgp

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/tsamsiyu/pontifex/api/v1alpha1"
	"github.com/tsamsiyu/pontifex/apps/agent/internal/libs/bgp"
)

// fakeServer is a test double for bgp.Server.
type fakeServer struct {
	neighbors  []bgp.Neighbor
	policies   []bgp.Policy
	advertised []bgp.Route

	startErr          error
	listNeighborsErr  error
	listPoliciesErr   error
	listAdvertisedErr error
	addNeighborErr    error
	addPolicyErr      error
	advertiseErr      error

	startCalls          int
	addNeighborCalls    []bgp.Neighbor
	removeNeighborCalls []string
	addPolicyCalls      []bgp.Policy
	removePolicyCalls   []string
	advertiseCalls      []bgp.Route
	withdrawCalls       []string
}

var _ bgp.Server = (*fakeServer)(nil)

func newFakeServer() *fakeServer {
	return &fakeServer{}
}

func (f *fakeServer) Start(_ context.Context, _ uint32, _ string) error {
	f.startCalls++
	return f.startErr
}

func (f *fakeServer) Stop(_ context.Context) error { return nil }

func (f *fakeServer) AddNeighbor(_ context.Context, n bgp.Neighbor) error {
	f.addNeighborCalls = append(f.addNeighborCalls, n)
	return f.addNeighborErr
}

func (f *fakeServer) RemoveNeighbor(_ context.Context, address string) error {
	f.removeNeighborCalls = append(f.removeNeighborCalls, address)
	return nil
}

func (f *fakeServer) ListNeighbors(_ context.Context) ([]bgp.Neighbor, error) {
	return f.neighbors, f.listNeighborsErr
}

func (f *fakeServer) AddPolicy(_ context.Context, p bgp.Policy) error {
	f.addPolicyCalls = append(f.addPolicyCalls, p)
	return f.addPolicyErr
}

func (f *fakeServer) RemovePolicy(_ context.Context, name string) error {
	f.removePolicyCalls = append(f.removePolicyCalls, name)
	return nil
}

func (f *fakeServer) ListPolicies(_ context.Context) ([]bgp.Policy, error) {
	return f.policies, f.listPoliciesErr
}

func (f *fakeServer) Advertise(_ context.Context, prefix string, communities []string) error {
	f.advertiseCalls = append(f.advertiseCalls, bgp.Route{Prefix: prefix, Communities: communities})
	return f.advertiseErr
}

func (f *fakeServer) Withdraw(_ context.Context, prefix string) error {
	f.withdrawCalls = append(f.withdrawCalls, prefix)
	return nil
}

func (f *fakeServer) ListAdvertised(_ context.Context) ([]bgp.Route, error) {
	return f.advertised, f.listAdvertisedErr
}

func (f *fakeServer) Subscribe() <-chan bgp.RouteEvent {
	ch := make(chan bgp.RouteEvent, 1)
	return ch
}

// hasNeighbor reports whether addr appears in addNeighborCalls.
func hasNeighbor(calls []bgp.Neighbor, addr string) (bgp.Neighbor, bool) {
	for _, n := range calls {
		if n.Address == addr {
			return n, true
		}
	}
	return bgp.Neighbor{}, false
}

// hasPolicy reports whether a policy with the given name appears in addPolicyCalls.
func hasPolicy(calls []bgp.Policy, name string) (bgp.Policy, bool) {
	for _, p := range calls {
		if p.Name == name {
			return p, true
		}
	}
	return bgp.Policy{}, false
}

// containsStr reports whether s appears in the slice.
func containsStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// mkOverlay constructs a NetworkOverlay for tests.
func mkOverlay(name, community string, peers []v1alpha1.Peer, gateways []v1alpha1.Gateway, edges []v1alpha1.EdgeStatus) v1alpha1.NetworkOverlay {
	return v1alpha1.NetworkOverlay{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       v1alpha1.NetworkOverlaySpec{Peers: peers},
		Status: v1alpha1.NetworkOverlayStatus{
			Community: community,
			Gateways:  gateways,
			Edges:     edges,
		},
	}
}
