package firewall

import "context"

// nftablesFirewall is the nftables-backed Firewall. Phase 1: stub.
type nftablesFirewall struct{}

// NewNFTables returns a Firewall backed by github.com/google/nftables.
func NewNFTables() Firewall {
	return &nftablesFirewall{}
}

func (f *nftablesFirewall) EnsureBridge(ctx context.Context, overlayName, virtualIP, podIP string) error {
	return errNotImplemented
}

func (f *nftablesFirewall) RemoveBridge(ctx context.Context, overlayName, virtualIP string) error {
	return errNotImplemented
}

func (f *nftablesFirewall) ListBridges(ctx context.Context) ([]Bridge, error) {
	return nil, errNotImplemented
}
