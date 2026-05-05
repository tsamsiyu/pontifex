package wireguard

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"

	v1alpha1 "github.com/tsamsiyu/pontifex/api/v1alpha1"
	"github.com/tsamsiyu/pontifex/apps/agent/internal/libs/wg"
)

const (
	ifacePrefix = "pntfx-"
	ifaceMaxLen = 15
)

// Reconciler ensures one WireGuard interface per overlay on a gateway host.
// The private key is loaded at reconcile time from
// <wgKeyDir>/<overlay>/private (mounted by the operator from the per-overlay
// Secret referenced in status.wgSecretRef).
type Reconciler struct {
	logger   *zap.Logger
	wg       wg.Manager
	wgKeyDir string
	listen   int
}

// New returns a Reconciler.
func New(mgr wg.Manager, wgKeyDir string, listenPort int, logger *zap.Logger) *Reconciler {
	return &Reconciler{logger: logger, wg: mgr, wgKeyDir: wgKeyDir, listen: listenPort}
}

// ifaceName returns the WireGuard interface name for an overlay.
// Linux interface names are limited to IFNAMSIZ-1 = 15 bytes.
func ifaceName(overlayName string) string {
	name := ifacePrefix + overlayName
	if len(name) > ifaceMaxLen {
		return name[:ifaceMaxLen]
	}
	return name
}

// Reconcile converges host WireGuard interfaces to match the overlay snapshot.
// For each overlay it reads the private key from <wgKeyDir>/<overlay>/private
// and calls EnsureInterface. Interfaces no longer present in the snapshot are
// removed. If a key file is unreadable the overlay is skipped (neither created
// nor removed) so a transient mount delay doesn't tear down a live interface.
func (r *Reconciler) Reconcile(ctx context.Context, overlays []v1alpha1.NetworkOverlay) error {
	// desired maps interface name → config; skip marks names to leave untouched.
	desired := make(map[string]wg.Interface, len(overlays))
	skip := make(map[string]struct{})
	var errs []error

	for _, overlay := range overlays {
		name := ifaceName(overlay.Name)
		privKeyBytes, err := os.ReadFile(filepath.Join(r.wgKeyDir, overlay.Name, "private"))
		if err != nil {
			r.logger.Error("reading wg private key, skipping overlay",
				zap.String("overlay", overlay.Name), zap.Error(err))
			skip[name] = struct{}{}
			errs = append(errs, fmt.Errorf("overlay %s: read private key: %w", overlay.Name, err))
			continue
		}

		peers := make([]wg.Peer, 0, len(overlay.Spec.Peers))
		for _, p := range overlay.Spec.Peers {
			peers = append(peers, wg.Peer{
				PublicKey:  p.PublicKey,
				Endpoint:   p.Endpoint,
				AllowedIPs: p.AllowedIPs,
			})
		}

		desired[name] = wg.Interface{
			Name:       name,
			PrivateKey: strings.TrimSpace(string(privKeyBytes)),
			ListenPort: r.listen,
			Peers:      peers,
		}
	}

	actual, err := r.wg.ListInterfaces(ctx)
	if err != nil {
		return fmt.Errorf("list wg interfaces: %w", err)
	}

	// Remove interfaces that are no longer desired and not skipped.
	for _, iface := range actual {
		if _, ok := desired[iface.Name]; ok {
			continue
		}
		if _, ok := skip[iface.Name]; ok {
			continue
		}
		if err := r.wg.RemoveInterface(ctx, iface.Name); err != nil {
			r.logger.Error("remove wg interface", zap.String("name", iface.Name), zap.Error(err))
			errs = append(errs, fmt.Errorf("remove interface %s: %w", iface.Name, err))
		} else {
			r.logger.Info("removed wg interface", zap.String("name", iface.Name))
		}
	}

	// Ensure all desired interfaces exist with the correct configuration.
	for _, iface := range desired {
		if err := r.wg.EnsureInterface(ctx, iface); err != nil {
			r.logger.Error("ensure wg interface", zap.String("name", iface.Name), zap.Error(err))
			errs = append(errs, fmt.Errorf("ensure interface %s: %w", iface.Name, err))
		}
	}

	return errors.Join(errs...)
}
