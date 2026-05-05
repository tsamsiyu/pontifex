package routes

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"go.uber.org/zap"

	v1alpha1 "github.com/tsamsiyu/pontifex/api/v1alpha1"
	"github.com/tsamsiyu/pontifex/apps/agent/internal/libs/bgp"
	"github.com/tsamsiyu/pontifex/apps/agent/internal/libs/firewall"
	"github.com/tsamsiyu/pontifex/apps/agent/internal/libs/routes"
)

// InternalReconciler manages per-overlay VRFs, virtual-IP addresses, ip rules,
// BGP-learned routes, and the iptables/nftables bridge that DNATs virtual IPs
// to local pod IPs.
type InternalReconciler struct {
	logger    *zap.Logger
	routes    routes.Routes
	firewall  firewall.Firewall
	bgpEvents <-chan bgp.RouteEvent
	nodeName  string
	learned   map[string]bgp.Route // key = prefix
}

// NewInternalReconciler returns an InternalReconciler.
func NewInternalReconciler(rt routes.Routes, fw firewall.Firewall, bgpEvents <-chan bgp.RouteEvent, nodeName string, logger *zap.Logger) *InternalReconciler {
	return &InternalReconciler{
		logger:    logger,
		routes:    rt,
		firewall:  fw,
		bgpEvents: bgpEvents,
		nodeName:  nodeName,
		learned:   make(map[string]bgp.Route),
	}
}

// vrfName returns the VRF device name for an overlay.
func vrfName(overlayName string) string {
	return "pntfx-" + overlayName
}

// vrfTableID parses a community string of the form "ASN:N" and returns
// 10000+N as the VRF routing table ID, keeping safely above system-reserved
// tables (0-255) and common protocol tables.
func vrfTableID(community string) (uint32, error) {
	parts := strings.SplitN(community, ":", 2)
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid community format %q", community)
	}
	n, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil {
		return 0, fmt.Errorf("parse community number in %q: %w", community, err)
	}
	return uint32(n) + 10000, nil
}

// Reconcile converges all host networking for internal-node overlays:
// VRFs, ip rules, virtual-IP addresses, BGP-learned routes, and firewall bridges.
func (r *InternalReconciler) Reconcile(ctx context.Context, overlays []v1alpha1.NetworkOverlay) error {
	r.drainBGPEvents()

	var errs []error

	if err := r.reconcileVRFs(ctx, overlays, &errs); err != nil {
		return err
	}
	if err := r.reconcileRules(ctx, overlays, &errs); err != nil {
		return err
	}
	r.reconcileAddrs(ctx, overlays, &errs)
	r.reconcileRoutes(ctx, overlays, &errs)
	if err := r.reconcileBridges(ctx, overlays, &errs); err != nil {
		return err
	}

	return errors.Join(errs...)
}

func (r *InternalReconciler) drainBGPEvents() {
	for {
		select {
		case ev := <-r.bgpEvents:
			if ev.Type == bgp.RouteWithdrawn {
				delete(r.learned, ev.Route.Prefix)
			} else {
				r.learned[ev.Route.Prefix] = ev.Route
			}
		default:
			return
		}
	}
}

// reconcileVRFs ensures the desired per-overlay VRF devices exist. Returns a
// non-nil error only on ListVRFs failure (caller should abort the reconcile).
func (r *InternalReconciler) reconcileVRFs(ctx context.Context, overlays []v1alpha1.NetworkOverlay, errs *[]error) error {
	desired := make(map[string]routes.VRF)
	for _, ov := range overlays {
		if ov.Status.Community == "" {
			continue
		}
		tid, err := vrfTableID(ov.Status.Community)
		if err != nil {
			r.logger.Warn("invalid community, skipping VRF", zap.String("overlay", ov.Name), zap.Error(err))
			continue
		}
		name := vrfName(ov.Name)
		desired[name] = routes.VRF{Name: name, TableID: tid}
	}

	actual, err := r.routes.ListVRFs(ctx)
	if err != nil {
		return fmt.Errorf("list VRFs: %w", err)
	}

	for _, vrf := range actual {
		if _, ok := desired[vrf.Name]; ok {
			continue
		}
		if err := r.routes.RemoveVRF(ctx, vrf.Name); err != nil {
			r.logger.Error("remove VRF", zap.String("name", vrf.Name), zap.Error(err))
			*errs = append(*errs, fmt.Errorf("remove VRF %s: %w", vrf.Name, err))
		}
	}

	actualNames := make(map[string]struct{}, len(actual))
	for _, vrf := range actual {
		actualNames[vrf.Name] = struct{}{}
	}

	for _, vrf := range desired {
		if _, ok := actualNames[vrf.Name]; ok {
			continue
		}
		if err := r.routes.EnsureVRF(ctx, vrf.Name, vrf.TableID); err != nil {
			r.logger.Error("ensure VRF", zap.String("name", vrf.Name), zap.Error(err))
			*errs = append(*errs, fmt.Errorf("ensure VRF %s: %w", vrf.Name, err))
		}
	}

	return nil
}

// reconcileRules ensures the desired per-overlay ip rules exist.
func (r *InternalReconciler) reconcileRules(ctx context.Context, overlays []v1alpha1.NetworkOverlay, errs *[]error) error {
	desired := make(map[uint32]routes.Rule) // key = tableID
	for _, ov := range overlays {
		if ov.Status.Community == "" || ov.Spec.VirtualCIDR == "" {
			continue
		}
		tid, err := vrfTableID(ov.Status.Community)
		if err != nil {
			continue
		}
		desired[tid] = routes.Rule{To: ov.Spec.VirtualCIDR, TableID: tid}
	}

	actual, err := r.routes.ListRules(ctx)
	if err != nil {
		return fmt.Errorf("list ip rules: %w", err)
	}

	for _, rule := range actual {
		if _, ok := desired[rule.TableID]; ok {
			continue
		}
		if err := r.routes.RemoveRule(ctx, rule); err != nil {
			r.logger.Error("remove ip rule", zap.Uint32("table", rule.TableID), zap.Error(err))
			*errs = append(*errs, fmt.Errorf("remove ip rule table %d: %w", rule.TableID, err))
		}
	}

	actualTables := make(map[uint32]struct{}, len(actual))
	for _, rule := range actual {
		actualTables[rule.TableID] = struct{}{}
	}

	for _, rule := range desired {
		if _, ok := actualTables[rule.TableID]; ok {
			continue
		}
		if err := r.routes.EnsureRule(ctx, rule); err != nil {
			r.logger.Error("ensure ip rule", zap.Uint32("table", rule.TableID), zap.Error(err))
			*errs = append(*errs, fmt.Errorf("ensure ip rule table %d: %w", rule.TableID, err))
		}
	}

	return nil
}

// reconcileAddrs ensures the per-VRF virtual-IP addresses for edges on this node.
func (r *InternalReconciler) reconcileAddrs(ctx context.Context, overlays []v1alpha1.NetworkOverlay, errs *[]error) {
	for _, ov := range overlays {
		if ov.Status.Community == "" {
			continue
		}
		vrf := vrfName(ov.Name)

		desiredIPs := make(map[string]struct{})
		for _, edge := range ov.Status.Edges {
			if edge.NodeName == r.nodeName && edge.Ready && edge.VirtualIP != "" {
				desiredIPs[edge.VirtualIP] = struct{}{}
			}
		}

		actual, err := r.routes.ListAddrs(ctx, vrf)
		if err != nil {
			r.logger.Error("list addrs", zap.String("vrf", vrf), zap.Error(err))
			*errs = append(*errs, fmt.Errorf("list addrs for VRF %s: %w", vrf, err))
			continue
		}

		for _, addr := range actual {
			if _, ok := desiredIPs[addr.IP]; ok {
				continue
			}
			if err := r.routes.RemoveAddr(ctx, vrf, addr.IP); err != nil {
				r.logger.Error("remove addr", zap.String("vrf", vrf), zap.String("ip", addr.IP), zap.Error(err))
				*errs = append(*errs, fmt.Errorf("remove addr %s from VRF %s: %w", addr.IP, vrf, err))
			}
		}

		actualIPs := make(map[string]struct{}, len(actual))
		for _, addr := range actual {
			actualIPs[addr.IP] = struct{}{}
		}

		for ip := range desiredIPs {
			if _, ok := actualIPs[ip]; ok {
				continue
			}
			if err := r.routes.EnsureAddr(ctx, vrf, ip); err != nil {
				r.logger.Error("ensure addr", zap.String("vrf", vrf), zap.String("ip", ip), zap.Error(err))
				*errs = append(*errs, fmt.Errorf("ensure addr %s on VRF %s: %w", ip, vrf, err))
			}
		}
	}
}

// reconcileRoutes syncs BGP-learned routes into each overlay's VRF table.
func (r *InternalReconciler) reconcileRoutes(ctx context.Context, overlays []v1alpha1.NetworkOverlay, errs *[]error) {
	type overlayInfo struct {
		name    string
		tableID uint32
	}

	communityMap := make(map[string]overlayInfo)
	for _, ov := range overlays {
		if ov.Status.Community == "" {
			continue
		}
		tid, err := vrfTableID(ov.Status.Community)
		if err != nil {
			continue
		}
		communityMap[ov.Status.Community] = overlayInfo{name: ov.Name, tableID: tid}
	}

	routesByTable := make(map[uint32][]routes.Route)
	for _, rt := range r.learned {
		for _, c := range rt.Communities {
			if info, ok := communityMap[c]; ok {
				routesByTable[info.tableID] = append(routesByTable[info.tableID], routes.Route{
					TableID: info.tableID,
					Dst:     rt.Prefix,
					Gw:      rt.NextHop,
				})
				break
			}
		}
	}

	for _, ov := range overlays {
		if ov.Status.Community == "" {
			continue
		}
		tid, err := vrfTableID(ov.Status.Community)
		if err != nil {
			continue
		}
		desired := routesByTable[tid]
		if err := r.routes.SyncRoutes(ctx, tid, desired); err != nil {
			r.logger.Error("sync routes", zap.String("overlay", ov.Name), zap.Uint32("table", tid), zap.Error(err))
			*errs = append(*errs, fmt.Errorf("sync routes for overlay %s table %d: %w", ov.Name, tid, err))
		}
	}
}

// reconcileBridges ensures firewall DNAT/SNAT bridges for ready edges on this node.
func (r *InternalReconciler) reconcileBridges(ctx context.Context, overlays []v1alpha1.NetworkOverlay, errs *[]error) error {
	type bridgeKey struct {
		overlayName string
		virtualIP   string
	}

	desired := make(map[bridgeKey]firewall.Bridge)
	for _, ov := range overlays {
		for _, edge := range ov.Status.Edges {
			if edge.NodeName != r.nodeName || !edge.Ready || edge.VirtualIP == "" || edge.PodIP == "" {
				continue
			}
			key := bridgeKey{overlayName: ov.Name, virtualIP: edge.VirtualIP}
			desired[key] = firewall.Bridge{
				OverlayName: ov.Name,
				VirtualIP:   edge.VirtualIP,
				PodIP:       edge.PodIP,
			}
		}
	}

	actual, err := r.firewall.ListBridges(ctx)
	if err != nil {
		return fmt.Errorf("list firewall bridges: %w", err)
	}

	for _, bridge := range actual {
		key := bridgeKey{overlayName: bridge.OverlayName, virtualIP: bridge.VirtualIP}
		if _, ok := desired[key]; ok {
			continue
		}
		if err := r.firewall.RemoveBridge(ctx, bridge.OverlayName, bridge.VirtualIP); err != nil {
			r.logger.Error("remove firewall bridge",
				zap.String("overlay", bridge.OverlayName),
				zap.String("vip", bridge.VirtualIP),
				zap.Error(err))
			*errs = append(*errs, fmt.Errorf("remove bridge %s/%s: %w", bridge.OverlayName, bridge.VirtualIP, err))
		}
	}

	actualKeys := make(map[bridgeKey]struct{}, len(actual))
	for _, bridge := range actual {
		actualKeys[bridgeKey{overlayName: bridge.OverlayName, virtualIP: bridge.VirtualIP}] = struct{}{}
	}

	for key, bridge := range desired {
		if _, ok := actualKeys[key]; ok {
			continue
		}
		if err := r.firewall.EnsureBridge(ctx, bridge.OverlayName, bridge.VirtualIP, bridge.PodIP); err != nil {
			r.logger.Error("ensure firewall bridge",
				zap.String("overlay", bridge.OverlayName),
				zap.String("vip", bridge.VirtualIP),
				zap.Error(err))
			*errs = append(*errs, fmt.Errorf("ensure bridge %s/%s: %w", bridge.OverlayName, bridge.VirtualIP, err))
		}
	}

	return nil
}
