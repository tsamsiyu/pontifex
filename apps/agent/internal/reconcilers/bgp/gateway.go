package bgp

import (
	"context"
	"errors"
	"fmt"
	"net"

	"go.uber.org/zap"

	v1alpha1 "github.com/tsamsiyu/pontifex/api/v1alpha1"
	"github.com/tsamsiyu/pontifex/apps/agent/internal/libs/bgp"
)

// GatewayReconciler runs a GoBGP server for the gateway role: route reflector
// to internal-node iBGP peers, eBGP to external peers over WireGuard, with
// per-overlay community import/export filters.
type GatewayReconciler struct {
	logger      *zap.Logger
	server      bgp.Server
	asn         uint32
	routerID    string
	isSecondary bool
	started     bool
}

// NewGatewayReconciler returns a GatewayReconciler.
func NewGatewayReconciler(server bgp.Server, asn uint32, routerID string, isSecondary bool, logger *zap.Logger) *GatewayReconciler {
	return &GatewayReconciler{
		logger:      logger,
		server:      server,
		asn:         asn,
		routerID:    routerID,
		isSecondary: isSecondary,
	}
}

// Subscribe returns the BGP route event channel for the routes reconciler.
func (r *GatewayReconciler) Subscribe() <-chan bgp.RouteEvent {
	return r.server.Subscribe()
}

// Reconcile converges local BGP neighbors and policies to the overlay snapshot.
// On first call it starts the BGP server. Each cycle it lists actual state,
// removes orphans, and adds missing neighbors/policies.
func (r *GatewayReconciler) Reconcile(ctx context.Context, overlays []v1alpha1.NetworkOverlay) error {
	if !r.started {
		if err := r.server.Start(ctx, r.asn, r.routerID); err != nil {
			return fmt.Errorf("start bgp server: %w", err)
		}
		r.started = true
	}

	desiredNeighbors := r.buildDesiredNeighbors(overlays)
	desiredPolicies := r.buildDesiredPolicies(overlays)

	actualNeighbors, err := r.server.ListNeighbors(ctx)
	if err != nil {
		return fmt.Errorf("list bgp neighbors: %w", err)
	}
	actualPolicies, err := r.server.ListPolicies(ctx)
	if err != nil {
		return fmt.Errorf("list bgp policies: %w", err)
	}

	var errs []error

	// Remove orphan neighbors first so they release any policy references.
	for _, n := range actualNeighbors {
		if _, ok := desiredNeighbors[n.Address]; ok {
			continue
		}
		if err := r.server.RemoveNeighbor(ctx, n.Address); err != nil {
			r.logger.Error("remove bgp neighbor", zap.String("address", n.Address), zap.Error(err))
			errs = append(errs, fmt.Errorf("remove neighbor %s: %w", n.Address, err))
		} else {
			r.logger.Info("removed bgp neighbor", zap.String("address", n.Address))
		}
	}

	// Remove orphan policies after neighbors.
	for _, p := range actualPolicies {
		if _, ok := desiredPolicies[p.Name]; ok {
			continue
		}
		if err := r.server.RemovePolicy(ctx, p.Name); err != nil {
			r.logger.Error("remove bgp policy", zap.String("name", p.Name), zap.Error(err))
			errs = append(errs, fmt.Errorf("remove policy %s: %w", p.Name, err))
		} else {
			r.logger.Info("removed bgp policy", zap.String("name", p.Name))
		}
	}

	// Index actual state for O(1) existence checks.
	actualNeighborAddrs := make(map[string]struct{}, len(actualNeighbors))
	for _, n := range actualNeighbors {
		actualNeighborAddrs[n.Address] = struct{}{}
	}
	actualPolicyNames := make(map[string]struct{}, len(actualPolicies))
	for _, p := range actualPolicies {
		actualPolicyNames[p.Name] = struct{}{}
	}

	// Add new policies before neighbors so policy references resolve.
	for _, p := range desiredPolicies {
		if _, ok := actualPolicyNames[p.Name]; ok {
			continue
		}
		if err := r.server.AddPolicy(ctx, p); err != nil {
			r.logger.Error("add bgp policy", zap.String("name", p.Name), zap.Error(err))
			errs = append(errs, fmt.Errorf("add policy %s: %w", p.Name, err))
		}
	}

	// Add new neighbors.
	for _, n := range desiredNeighbors {
		if _, ok := actualNeighborAddrs[n.Address]; ok {
			continue
		}
		if err := r.server.AddNeighbor(ctx, n); err != nil {
			r.logger.Error("add bgp neighbor", zap.String("address", n.Address), zap.Error(err))
			errs = append(errs, fmt.Errorf("add neighbor %s: %w", n.Address, err))
		}
	}

	return errors.Join(errs...)
}

// buildDesiredNeighbors computes the full neighbor set from the snapshot.
// Keys are BGP peer addresses. Overlays without a community are skipped.
// Duplicate peer addresses across overlays use the first occurrence.
func (r *GatewayReconciler) buildDesiredNeighbors(overlays []v1alpha1.NetworkOverlay) map[string]bgp.Neighbor {
	desired := make(map[string]bgp.Neighbor)

	for _, overlay := range overlays {
		if overlay.Status.Community == "" {
			r.logger.Warn("overlay has no community, skipping", zap.String("overlay", overlay.Name))
			continue
		}

		policies := []string{
			"pntfx-import-" + overlay.Name,
			"pntfx-export-" + overlay.Name,
		}
		if r.isSecondary {
			policies = append(policies, "pntfx-prepend-"+overlay.Name)
		}

		for _, peer := range overlay.Spec.Peers {
			addr, err := peerAddress(peer.Endpoint)
			if err != nil {
				r.logger.Warn("invalid peer endpoint, skipping",
					zap.String("overlay", overlay.Name),
					zap.String("peer", peer.Name),
					zap.Error(err))
				continue
			}
			if _, exists := desired[addr]; exists {
				r.logger.Warn("duplicate peer address across overlays, keeping first",
					zap.String("address", addr),
					zap.String("overlay", overlay.Name),
					zap.String("peer", peer.Name))
				continue
			}
			desired[addr] = bgp.Neighbor{
				Name:     "pntfx-peer-" + overlay.Name + "-" + peer.Name,
				Address:  addr,
				ASN:      peer.ASN,
				IsIBGP:   false,
				Policies: policies,
			}
		}

		// iBGP route-reflector clients — one per unique internal-node IP.
		for _, edge := range overlay.Status.Edges {
			if !edge.Ready || edge.NodeIP == "" {
				continue
			}
			if _, exists := desired[edge.NodeIP]; exists {
				continue
			}
			desired[edge.NodeIP] = bgp.Neighbor{
				Name:    "pntfx-internal-" + edge.NodeIP,
				Address: edge.NodeIP,
				ASN:     r.asn,
				IsIBGP:  true,
			}
		}
	}

	return desired
}

// buildDesiredPolicies computes per-overlay import/export (and optionally
// prepend) policies. Overlays without a community are skipped.
func (r *GatewayReconciler) buildDesiredPolicies(overlays []v1alpha1.NetworkOverlay) map[string]bgp.Policy {
	desired := make(map[string]bgp.Policy)

	for _, overlay := range overlays {
		if overlay.Status.Community == "" {
			continue
		}
		community := overlay.Status.Community

		desired["pntfx-import-"+overlay.Name] = bgp.Policy{
			Name:      "pntfx-import-" + overlay.Name,
			Direction: bgp.PolicyImport,
			Match:     "any",
			Action:    "community-set:" + community,
		}
		desired["pntfx-export-"+overlay.Name] = bgp.Policy{
			Name:      "pntfx-export-" + overlay.Name,
			Direction: bgp.PolicyExport,
			Match:     "community:" + community,
			Action:    "accept",
		}
		if r.isSecondary {
			desired["pntfx-prepend-"+overlay.Name] = bgp.Policy{
				Name:      "pntfx-prepend-" + overlay.Name,
				Direction: bgp.PolicyExport,
				Match:     "community:" + community,
				Action:    "prepend-asn:3",
			}
		}
	}

	return desired
}

// peerAddress extracts the host portion from a "host:port" endpoint string.
// A bare IP address (no port) is returned as-is.
func peerAddress(endpoint string) (string, error) {
	host, _, err := net.SplitHostPort(endpoint)
	if err == nil {
		return host, nil
	}
	if net.ParseIP(endpoint) != nil {
		return endpoint, nil
	}
	return "", fmt.Errorf("parse endpoint %q: %w", endpoint, err)
}
