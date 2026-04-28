package wg

import (
	"context"
	"errors"
)

var errNotImplemented = errors.New("not implemented")

// wgctrlManager is the wgctrl-go-backed Manager. Phase 1: stub.
type wgctrlManager struct{}

// NewWGCtrlManager returns a Manager backed by golang.zx2c4.com/wireguard/wgctrl.
func NewWGCtrlManager() Manager {
	return &wgctrlManager{}
}

func (m *wgctrlManager) EnsureInterface(ctx context.Context, iface Interface) error {
	return errNotImplemented
}

func (m *wgctrlManager) RemoveInterface(ctx context.Context, name string) error {
	return errNotImplemented
}

func (m *wgctrlManager) ListInterfaces(ctx context.Context) ([]Interface, error) {
	return nil, errNotImplemented
}
