package bgp

import (
	"context"
	"errors"
)

// errNotImplemented is the placeholder returned by the Phase 1 stub.
var errNotImplemented = errors.New("not implemented")

// gobgpServer is the GoBGP-backed Server. Phase 1: stub.
type gobgpServer struct {
	events chan RouteEvent
}

// NewGoBGPServer returns a Server backed by github.com/osrg/gobgp/v3.
func NewGoBGPServer() Server {
	return &gobgpServer{events: make(chan RouteEvent, 64)}
}

func (s *gobgpServer) Start(ctx context.Context, asn uint32, routerID string) error {
	return errNotImplemented
}

func (s *gobgpServer) Stop(ctx context.Context) error {
	return errNotImplemented
}

func (s *gobgpServer) AddNeighbor(ctx context.Context, n Neighbor) error {
	return errNotImplemented
}

func (s *gobgpServer) RemoveNeighbor(ctx context.Context, address string) error {
	return errNotImplemented
}

func (s *gobgpServer) ListNeighbors(ctx context.Context) ([]Neighbor, error) {
	return nil, errNotImplemented
}

func (s *gobgpServer) AddPolicy(ctx context.Context, p Policy) error {
	return errNotImplemented
}

func (s *gobgpServer) RemovePolicy(ctx context.Context, name string) error {
	return errNotImplemented
}

func (s *gobgpServer) ListPolicies(ctx context.Context) ([]Policy, error) {
	return nil, errNotImplemented
}

func (s *gobgpServer) Advertise(ctx context.Context, prefix string, communities []string) error {
	return errNotImplemented
}

func (s *gobgpServer) Withdraw(ctx context.Context, prefix string) error {
	return errNotImplemented
}

func (s *gobgpServer) ListAdvertised(ctx context.Context) ([]Route, error) {
	return nil, errNotImplemented
}

func (s *gobgpServer) Subscribe() <-chan RouteEvent {
	return s.events
}
