package routes

import "context"

// Routes manages VRFs, addresses on the per-VRF dummy, ip rules, and routes
// in per-VRF tables. Listed entities are filtered to those this agent owns:
//   - VRFs: name has the "pntfx-" prefix.
//   - Addresses: only those on a managed VRF dummy.
//   - Rules: only those whose lookup-table is in the managed VRF tables set.
//   - Routes: only those whose proto is "pontifex".
type Routes interface {
	EnsureVRF(ctx context.Context, name string, tableID uint32) error
	RemoveVRF(ctx context.Context, name string) error
	ListVRFs(ctx context.Context) ([]VRF, error)

	EnsureAddr(ctx context.Context, vrfName, ip string) error
	RemoveAddr(ctx context.Context, vrfName, ip string) error
	ListAddrs(ctx context.Context, vrfName string) ([]Addr, error)

	EnsureRule(ctx context.Context, r Rule) error
	RemoveRule(ctx context.Context, r Rule) error
	ListRules(ctx context.Context) ([]Rule, error)

	AddRoute(ctx context.Context, tableID uint32, dst, gw, dev string) error
	RemoveRoute(ctx context.Context, tableID uint32, dst string) error
	SyncRoutes(ctx context.Context, tableID uint32, desired []Route) error
	ListRoutes(ctx context.Context, tableID uint32) ([]Route, error)
}

// VRF is a managed VRF + dummy pair.
type VRF struct {
	Name    string
	TableID uint32
}

// Addr is an IP address on a VRF dummy.
type Addr struct {
	VRFName string
	IP      string
}

// Rule is an ip-rule entry.
type Rule struct {
	From    string
	To      string
	TableID uint32
	Iif     string
	Oif     string
}

// Route is a route entry in a VRF table.
type Route struct {
	TableID uint32
	Dst     string
	Gw      string
	Dev     string
}
