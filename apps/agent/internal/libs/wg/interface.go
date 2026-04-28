package wg

import "context"

// Manager owns the set of WireGuard interfaces this agent manages. Listed
// interfaces are filtered by the "pntfx-" name prefix so we never report or
// touch interfaces owned by other actors on the host.
type Manager interface {
	EnsureInterface(ctx context.Context, iface Interface) error
	RemoveInterface(ctx context.Context, name string) error
	ListInterfaces(ctx context.Context) ([]Interface, error)
}

// Interface is a single WireGuard interface configuration.
type Interface struct {
	Name       string
	PrivateKey string
	ListenPort int
	Peers      []Peer
}

// Peer is a WireGuard peer.
type Peer struct {
	PublicKey  string
	Endpoint   string
	AllowedIPs []string
}
