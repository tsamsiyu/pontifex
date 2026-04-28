package firewall

import (
	"context"
	"errors"
)

var errNotImplemented = errors.New("not implemented")

// iptablesFirewall is the iptables-backed Firewall. Phase 1: stub.
type iptablesFirewall struct{}

// NewIPTables returns a Firewall backed by github.com/coreos/go-iptables.
func NewIPTables() Firewall {
	return &iptablesFirewall{}
}

func (f *iptablesFirewall) EnsureBridge(ctx context.Context, overlayName, virtualIP, podIP string) error {
	return errNotImplemented
}

func (f *iptablesFirewall) RemoveBridge(ctx context.Context, overlayName, virtualIP string) error {
	return errNotImplemented
}

func (f *iptablesFirewall) ListBridges(ctx context.Context) ([]Bridge, error) {
	return nil, errNotImplemented
}
