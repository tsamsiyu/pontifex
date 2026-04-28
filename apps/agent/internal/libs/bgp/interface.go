package bgp

import "context"

// Server is the contract for the local BGP daemon. Backed by GoBGP. All
// state-mutating methods are idempotent.
type Server interface {
	Start(ctx context.Context, asn uint32, routerID string) error
	Stop(ctx context.Context) error

	AddNeighbor(ctx context.Context, n Neighbor) error
	RemoveNeighbor(ctx context.Context, address string) error
	ListNeighbors(ctx context.Context) ([]Neighbor, error)

	AddPolicy(ctx context.Context, p Policy) error
	RemovePolicy(ctx context.Context, name string) error
	ListPolicies(ctx context.Context) ([]Policy, error)

	Advertise(ctx context.Context, prefix string, communities []string) error
	Withdraw(ctx context.Context, prefix string) error
	ListAdvertised(ctx context.Context) ([]Route, error)

	Subscribe() <-chan RouteEvent
}

// Neighbor is a BGP neighbor configuration.
type Neighbor struct {
	Name     string
	Address  string
	ASN      uint32
	IsIBGP   bool
	NextHop  string
	Policies []string
}

// Policy is a named import/export policy.
type Policy struct {
	Name      string
	Direction PolicyDirection
	Match     string
	Action    string
}

// PolicyDirection selects import or export side.
type PolicyDirection int

const (
	PolicyImport PolicyDirection = iota
	PolicyExport
)

// Route is a BGP route entry.
type Route struct {
	Prefix      string
	NextHop     string
	Communities []string
}

// RouteEvent is a notification about a learned route changing.
type RouteEvent struct {
	Type  RouteEventType
	Route Route
}

// RouteEventType classifies a RouteEvent.
type RouteEventType int

const (
	RouteAdded RouteEventType = iota
	RouteWithdrawn
)
