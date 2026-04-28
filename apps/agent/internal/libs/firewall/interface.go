package firewall

import "context"

// Firewall installs the DNAT/SNAT bridge that maps a virtual IP to a local
// pod IP for one overlay edge. All rules and chains are tagged with
//
//	pontifex:overlay=<name>:vip=<ip>
//
// (iptables --comment, nftables meta) so ListBridges can reconstruct the
// managed set on agent restart.
type Firewall interface {
	EnsureBridge(ctx context.Context, overlayName, virtualIP, podIP string) error
	RemoveBridge(ctx context.Context, overlayName, virtualIP string) error
	ListBridges(ctx context.Context) ([]Bridge, error)
}

// Bridge is a single virtual-IP ↔ pod-IP bridge.
type Bridge struct {
	OverlayName string
	VirtualIP   string
	PodIP       string
}

// Backend selects between iptables and nftables.
type Backend int

const (
	BackendAuto Backend = iota
	BackendIPTables
	BackendNFTables
)
