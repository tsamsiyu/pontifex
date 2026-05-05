package routes

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	v1alpha1 "github.com/tsamsiyu/pontifex/api/v1alpha1"
	"github.com/tsamsiyu/pontifex/apps/agent/internal/libs/bgp"
	"github.com/tsamsiyu/pontifex/apps/agent/internal/libs/routes"
)

// GatewayReconciler installs peer-learned routes globally on the gateway host.
// Gateways do not host pods so VRF-per-overlay is unnecessary.
type GatewayReconciler struct {
	logger  *zap.Logger
	routes  routes.Routes
	learned map[string]bgp.Route // key = prefix
}

// NewGatewayReconciler returns a GatewayReconciler.
func NewGatewayReconciler(rt routes.Routes, logger *zap.Logger) *GatewayReconciler {
	return &GatewayReconciler{
		logger:  logger,
		routes:  rt,
		learned: make(map[string]bgp.Route),
	}
}

// ApplyEvent updates the internal learned-routes map from a BGP route event.
// Called by RoutesUpdater before each Reconcile triggered by a BGP event.
func (r *GatewayReconciler) ApplyEvent(ev bgp.RouteEvent) {
	if ev.Type == bgp.RouteWithdrawn {
		delete(r.learned, ev.Route.Prefix)
	} else {
		r.learned[ev.Route.Prefix] = ev.Route
	}
}

// Reconcile syncs the global routing table to contain exactly the learned
// routes whose community belongs to an active overlay.
func (r *GatewayReconciler) Reconcile(ctx context.Context, overlays []v1alpha1.NetworkOverlay) error {
	active := activeCommunities(overlays)
	desired := r.buildDesiredRoutes(active)

	if err := r.routes.SyncRoutes(ctx, 0, desired); err != nil {
		return fmt.Errorf("sync global routes: %w", err)
	}
	return nil
}

// buildDesiredRoutes returns all learned routes whose community intersects the
// active community set, translated to routes.Route for the global table.
func (r *GatewayReconciler) buildDesiredRoutes(active map[string]struct{}) []routes.Route {
	desired := make([]routes.Route, 0, len(r.learned))
	for _, rt := range r.learned {
		if routeMatchesCommunities(rt, active) {
			desired = append(desired, routes.Route{
				TableID: 0,
				Dst:     rt.Prefix,
				Gw:      rt.NextHop,
			})
		}
	}
	return desired
}

// activeCommunities returns the set of non-empty communities from the snapshot.
func activeCommunities(overlays []v1alpha1.NetworkOverlay) map[string]struct{} {
	m := make(map[string]struct{}, len(overlays))
	for _, ov := range overlays {
		if ov.Status.Community != "" {
			m[ov.Status.Community] = struct{}{}
		}
	}
	return m
}

// routeMatchesCommunities returns true if any of the route's communities is in active.
func routeMatchesCommunities(rt bgp.Route, active map[string]struct{}) bool {
	for _, c := range rt.Communities {
		if _, ok := active[c]; ok {
			return true
		}
	}
	return false
}
