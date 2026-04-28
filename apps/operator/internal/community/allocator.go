package community

import (
	"errors"
	"fmt"

	v1alpha1 "github.com/tsamsiyu/pontifex/api/v1alpha1"
)

var errNotImplemented = errors.New("not implemented")

// Allocator hands out unique BGP community values of the form "<asn>:<n>"
// from a single shared pool. The pool is reconstructed at startup from the
// existing NetworkOverlay set; allocations never overlap.
type Allocator struct {
	asn  uint32
	used map[string]struct{}
}

// New returns an Allocator seeded from the existing overlays' status.community.
func New(asn uint32, existing []v1alpha1.NetworkOverlay) *Allocator {
	a := &Allocator{asn: asn, used: make(map[string]struct{})}
	for i := range existing {
		if c := existing[i].Status.Community; c != "" {
			a.used[c] = struct{}{}
		}
	}
	return a
}

// Allocate returns the next free community.
func (a *Allocator) Allocate() (string, error) {
	for n := uint32(1); n < (1 << 16); n++ {
		c := fmt.Sprintf("%d:%d", a.asn, n)
		if _, ok := a.used[c]; ok {
			continue
		}
		a.used[c] = struct{}{}
		return c, nil
	}
	return "", errNotImplemented
}

// Release returns a community to the pool. Idempotent.
func (a *Allocator) Release(community string) {
	delete(a.used, community)
}
