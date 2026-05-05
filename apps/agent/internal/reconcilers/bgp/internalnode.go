package bgp

import (
	"context"
	"errors"
	"fmt"

	"go.uber.org/zap"

	v1alpha1 "github.com/tsamsiyu/pontifex/api/v1alpha1"
	"github.com/tsamsiyu/pontifex/apps/agent/internal/libs/bgp"
)

// InternalReconciler runs a GoBGP server for the internal-node role: iBGP to
// every entry in status.gateways, advertising local edge VirtualIPs with the
// overlay community and importing peer-learned routes into per-overlay VRFs.
// The BGP server must already be started before the first Reconcile call.
type InternalReconciler struct {
	logger   *zap.Logger
	server   bgp.Server
	asn      uint32
	routerID string
	nodeName string
}

// NewInternalReconciler returns an InternalReconciler.
func NewInternalReconciler(server bgp.Server, asn uint32, routerID string, nodeName string, logger *zap.Logger) *InternalReconciler {
	return &InternalReconciler{
		logger:   logger,
		server:   server,
		asn:      asn,
		routerID: routerID,
		nodeName: nodeName,
	}
}

// Subscribe returns the BGP route event channel for the routes reconciler.
func (r *InternalReconciler) Subscribe() <-chan bgp.RouteEvent {
	return r.server.Subscribe()
}

// Reconcile converges local BGP neighbors, policies, and advertisements to
// the overlay snapshot for this internal node.
func (r *InternalReconciler) Reconcile(ctx context.Context, overlays []v1alpha1.NetworkOverlay) error {
	desiredNeighbors := r.buildDesiredNeighbors(overlays)
	desiredPolicies := r.buildDesiredPolicies(overlays)
	desiredAds := r.buildDesiredAdvertisements(overlays)

	actualNeighbors, err := r.server.ListNeighbors(ctx)
	if err != nil {
		return fmt.Errorf("list bgp neighbors: %w", err)
	}
	actualPolicies, err := r.server.ListPolicies(ctx)
	if err != nil {
		return fmt.Errorf("list bgp policies: %w", err)
	}
	actualAdvertised, err := r.server.ListAdvertised(ctx)
	if err != nil {
		return fmt.Errorf("list bgp advertised: %w", err)
	}

	var errs []error

	// Remove orphans first.
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

	for _, rt := range actualAdvertised {
		if _, ok := desiredAds[rt.Prefix]; ok {
			continue
		}
		if err := r.server.Withdraw(ctx, rt.Prefix); err != nil {
			r.logger.Error("withdraw bgp route", zap.String("prefix", rt.Prefix), zap.Error(err))
			errs = append(errs, fmt.Errorf("withdraw %s: %w", rt.Prefix, err))
		} else {
			r.logger.Info("withdrew bgp route", zap.String("prefix", rt.Prefix))
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
	actualPrefixes := make(map[string]struct{}, len(actualAdvertised))
	for _, rt := range actualAdvertised {
		actualPrefixes[rt.Prefix] = struct{}{}
	}

	// Add new policies first so neighbor references resolve.
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

	// Advertise new prefixes.
	for _, ad := range desiredAds {
		if _, ok := actualPrefixes[ad.Prefix]; ok {
			continue
		}
		if err := r.server.Advertise(ctx, ad.Prefix, ad.Communities); err != nil {
			r.logger.Error("advertise bgp route", zap.String("prefix", ad.Prefix), zap.Error(err))
			errs = append(errs, fmt.Errorf("advertise %s: %w", ad.Prefix, err))
		}
	}

	return errors.Join(errs...)
}

// buildDesiredNeighbors returns one iBGP neighbor per unique gateway NodeIP
// across all overlays.
func (r *InternalReconciler) buildDesiredNeighbors(overlays []v1alpha1.NetworkOverlay) map[string]bgp.Neighbor {
	desired := make(map[string]bgp.Neighbor)
	for _, overlay := range overlays {
		for _, gw := range overlay.Status.Gateways {
			if gw.NodeIP == "" {
				continue
			}
			if _, exists := desired[gw.NodeIP]; exists {
				continue
			}
			desired[gw.NodeIP] = bgp.Neighbor{
				Name:    "pntfx-gateway-" + gw.NodeIP,
				Address: gw.NodeIP,
				ASN:     r.asn,
				IsIBGP:  true,
			}
		}
	}
	return desired
}

// buildDesiredPolicies returns per-overlay export (advertise VIPs with
// community) and import (accept peer-learned routes tagged with community)
// policies. Overlays without a community are skipped.
func (r *InternalReconciler) buildDesiredPolicies(overlays []v1alpha1.NetworkOverlay) map[string]bgp.Policy {
	desired := make(map[string]bgp.Policy)
	for _, overlay := range overlays {
		if overlay.Status.Community == "" {
			continue
		}
		community := overlay.Status.Community
		desired["pntfx-export-"+overlay.Name] = bgp.Policy{
			Name:      "pntfx-export-" + overlay.Name,
			Direction: bgp.PolicyExport,
			Match:     "any",
			Action:    "community-set:" + community,
		}
		desired["pntfx-import-"+overlay.Name] = bgp.Policy{
			Name:      "pntfx-import-" + overlay.Name,
			Direction: bgp.PolicyImport,
			Match:     "community:" + community,
			Action:    "accept",
		}
	}
	return desired
}

// buildDesiredAdvertisements returns a VirtualIP/32 advertisement for each
// edge on this node that is Ready and has a non-empty VirtualIP. The route
// carries the overlay community so gateways can apply correct export filters.
func (r *InternalReconciler) buildDesiredAdvertisements(overlays []v1alpha1.NetworkOverlay) map[string]bgp.Route {
	desired := make(map[string]bgp.Route)
	for _, overlay := range overlays {
		if overlay.Status.Community == "" {
			continue
		}
		for _, edge := range overlay.Status.Edges {
			if edge.NodeName != r.nodeName || !edge.Ready || edge.VirtualIP == "" {
				continue
			}
			prefix := edge.VirtualIP + "/32"
			desired[prefix] = bgp.Route{
				Prefix:      prefix,
				Communities: []string{overlay.Status.Community},
			}
		}
	}
	return desired
}
