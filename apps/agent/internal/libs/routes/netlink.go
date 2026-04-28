package routes

import (
	"context"
	"errors"
)

var errNotImplemented = errors.New("not implemented")

// netlinkRoutes is the netlink-backed Routes. Phase 1: stub.
type netlinkRoutes struct{}

// NewNetlinkRoutes returns a Routes backed by github.com/vishvananda/netlink.
func NewNetlinkRoutes() Routes {
	return &netlinkRoutes{}
}

func (r *netlinkRoutes) EnsureVRF(ctx context.Context, name string, tableID uint32) error {
	return errNotImplemented
}

func (r *netlinkRoutes) RemoveVRF(ctx context.Context, name string) error {
	return errNotImplemented
}

func (r *netlinkRoutes) ListVRFs(ctx context.Context) ([]VRF, error) {
	return nil, errNotImplemented
}

func (r *netlinkRoutes) EnsureAddr(ctx context.Context, vrfName, ip string) error {
	return errNotImplemented
}

func (r *netlinkRoutes) RemoveAddr(ctx context.Context, vrfName, ip string) error {
	return errNotImplemented
}

func (r *netlinkRoutes) ListAddrs(ctx context.Context, vrfName string) ([]Addr, error) {
	return nil, errNotImplemented
}

func (r *netlinkRoutes) EnsureRule(ctx context.Context, rule Rule) error {
	return errNotImplemented
}

func (r *netlinkRoutes) RemoveRule(ctx context.Context, rule Rule) error {
	return errNotImplemented
}

func (r *netlinkRoutes) ListRules(ctx context.Context) ([]Rule, error) {
	return nil, errNotImplemented
}

func (r *netlinkRoutes) AddRoute(ctx context.Context, tableID uint32, dst, gw, dev string) error {
	return errNotImplemented
}

func (r *netlinkRoutes) RemoveRoute(ctx context.Context, tableID uint32, dst string) error {
	return errNotImplemented
}

func (r *netlinkRoutes) SyncRoutes(ctx context.Context, tableID uint32, desired []Route) error {
	return errNotImplemented
}

func (r *netlinkRoutes) ListRoutes(ctx context.Context, tableID uint32) ([]Route, error) {
	return nil, errNotImplemented
}
