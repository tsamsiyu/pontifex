package wireguard

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"go.uber.org/zap"

	v1alpha1 "github.com/tsamsiyu/pontifex/api/v1alpha1"
	"github.com/tsamsiyu/pontifex/apps/agent/internal/libs/wg"
)

// fakeWG is a test double for wg.Manager.
type fakeWG struct {
	interfaces  []wg.Interface
	listErr     error
	ensureErr   error
	removeErr   error
	ensureCalls []wg.Interface
	removeCalls []string
}

func (f *fakeWG) ListInterfaces(_ context.Context) ([]wg.Interface, error) {
	return f.interfaces, f.listErr
}

func (f *fakeWG) EnsureInterface(_ context.Context, iface wg.Interface) error {
	f.ensureCalls = append(f.ensureCalls, iface)
	return f.ensureErr
}

func (f *fakeWG) RemoveInterface(_ context.Context, name string) error {
	f.removeCalls = append(f.removeCalls, name)
	return f.removeErr
}

var _ wg.Manager = (*fakeWG)(nil)

func newTestReconciler(f *fakeWG, keyDir string) *Reconciler {
	return New(f, keyDir, 51820, zap.NewNop())
}

// writeKey creates <dir>/<overlayName>/private with the given content.
func writeKey(t *testing.T, dir, overlayName, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, overlayName), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, overlayName, "private"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func overlay(name string, peers ...v1alpha1.Peer) v1alpha1.NetworkOverlay {
	return v1alpha1.NetworkOverlay{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       v1alpha1.NetworkOverlaySpec{Peers: peers},
	}
}

// ── ifaceName ────────────────────────────────────────────────────────────────

func TestIfaceName_Short(t *testing.T) {
	if got := ifaceName("abc"); got != "pntfx-abc" {
		t.Errorf("got %q, want pntfx-abc", got)
	}
}

func TestIfaceName_ExactLimit(t *testing.T) {
	// "pntfx-" (6) + 9 chars = 15 exactly — no truncation.
	got := ifaceName("123456789")
	if got != "pntfx-123456789" {
		t.Errorf("got %q, want pntfx-123456789", got)
	}
}

func TestIfaceName_Truncated(t *testing.T) {
	got := ifaceName("my-long-overlay-name")
	if len(got) > ifaceMaxLen {
		t.Errorf("len=%d exceeds %d: %q", len(got), ifaceMaxLen, got)
	}
	if got != "pntfx-my-long-o" {
		t.Errorf("got %q, want pntfx-my-long-o", got)
	}
}

// ── Reconcile: no-op cases ────────────────────────────────────────────────────

func TestReconcile_NoOverlaysNoInterfaces(t *testing.T) {
	f := &fakeWG{}
	r := newTestReconciler(f, t.TempDir())

	if err := r.Reconcile(context.Background(), nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.ensureCalls) != 0 || len(f.removeCalls) != 0 {
		t.Errorf("expected no calls; ensure=%d remove=%d", len(f.ensureCalls), len(f.removeCalls))
	}
}

// ── Reconcile: orphan removal ─────────────────────────────────────────────────

func TestReconcile_OrphanInterfaceRemoved(t *testing.T) {
	f := &fakeWG{interfaces: []wg.Interface{{Name: "pntfx-gone"}}}
	r := newTestReconciler(f, t.TempDir())

	if err := r.Reconcile(context.Background(), nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.removeCalls) != 1 || f.removeCalls[0] != "pntfx-gone" {
		t.Errorf("removeCalls=%v, want [pntfx-gone]", f.removeCalls)
	}
	if len(f.ensureCalls) != 0 {
		t.Errorf("no EnsureInterface expected, got %d calls", len(f.ensureCalls))
	}
}

// ── Reconcile: ensure path ────────────────────────────────────────────────────

func TestReconcile_EnsureCalledForOverlay(t *testing.T) {
	keyDir := t.TempDir()
	writeKey(t, keyDir, "myoverlay", "privatekey123\n")

	f := &fakeWG{}
	r := newTestReconciler(f, keyDir)

	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{overlay("myoverlay")}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.ensureCalls) != 1 {
		t.Fatalf("ensureCalls=%d, want 1", len(f.ensureCalls))
	}
	iface := f.ensureCalls[0]
	if iface.Name != "pntfx-myoverlay" {
		t.Errorf("Name=%q, want pntfx-myoverlay", iface.Name)
	}
	// trailing newline must be stripped
	if iface.PrivateKey != "privatekey123" {
		t.Errorf("PrivateKey=%q, want privatekey123", iface.PrivateKey)
	}
	if iface.ListenPort != 51820 {
		t.Errorf("ListenPort=%d, want 51820", iface.ListenPort)
	}
}

func TestReconcile_PeersPassedThrough(t *testing.T) {
	keyDir := t.TempDir()
	writeKey(t, keyDir, "ov", "key")

	f := &fakeWG{}
	r := newTestReconciler(f, keyDir)

	peer := v1alpha1.Peer{
		PublicKey:  "pubk1",
		Endpoint:   "1.2.3.4:51820",
		AllowedIPs: []string{"10.0.0.0/8"},
	}
	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{overlay("ov", peer)}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.ensureCalls[0].Peers) != 1 {
		t.Fatalf("peers=%d, want 1", len(f.ensureCalls[0].Peers))
	}
	p := f.ensureCalls[0].Peers[0]
	if p.PublicKey != "pubk1" || p.Endpoint != "1.2.3.4:51820" || p.AllowedIPs[0] != "10.0.0.0/8" {
		t.Errorf("unexpected peer: %+v", p)
	}
}

func TestReconcile_ExistingInterfaceNotRemovedWhenInDesired(t *testing.T) {
	keyDir := t.TempDir()
	writeKey(t, keyDir, "ov", "key")

	f := &fakeWG{interfaces: []wg.Interface{{Name: "pntfx-ov"}}}
	r := newTestReconciler(f, keyDir)

	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{overlay("ov")}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.removeCalls) != 0 {
		t.Errorf("removeCalls=%v, expected none", f.removeCalls)
	}
	if len(f.ensureCalls) != 1 {
		t.Errorf("ensureCalls=%d, want 1", len(f.ensureCalls))
	}
}

// ── Reconcile: missing key (skip set) ────────────────────────────────────────

func TestReconcile_MissingKeySkipsOverlay(t *testing.T) {
	// Existing interface for the overlay must not be removed when key is unreadable.
	f := &fakeWG{interfaces: []wg.Interface{{Name: "pntfx-ov"}}}
	r := newTestReconciler(f, t.TempDir()) // no key written

	err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{overlay("ov")})
	if err == nil {
		t.Fatal("expected error for missing key, got nil")
	}
	if len(f.removeCalls) != 0 {
		t.Errorf("removeCalls=%v; skipped overlay's interface must not be removed", f.removeCalls)
	}
	if len(f.ensureCalls) != 0 {
		t.Errorf("ensureCalls=%v; skipped overlay must not be ensured", f.ensureCalls)
	}
}

func TestReconcile_PartialKeyError_OtherOverlayStillReconciled(t *testing.T) {
	keyDir := t.TempDir()
	// ov1 has no key; ov2 does
	writeKey(t, keyDir, "ov2", "key2")

	f := &fakeWG{}
	r := newTestReconciler(f, keyDir)

	err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{overlay("ov1"), overlay("ov2")})
	if err == nil {
		t.Fatal("expected error for missing ov1 key")
	}
	ensuredNames := make([]string, len(f.ensureCalls))
	for i, c := range f.ensureCalls {
		ensuredNames[i] = c.Name
	}
	found := false
	for _, n := range ensuredNames {
		if n == "pntfx-ov2" {
			found = true
		}
	}
	if !found {
		t.Errorf("pntfx-ov2 not ensured (%v); ov1 key error must not block ov2", ensuredNames)
	}
}

// ── Reconcile: error propagation ─────────────────────────────────────────────

func TestReconcile_ListInterfacesError(t *testing.T) {
	listErr := errors.New("netlink down")
	f := &fakeWG{listErr: listErr}
	r := newTestReconciler(f, t.TempDir())

	err := r.Reconcile(context.Background(), nil)
	if !errors.Is(err, listErr) {
		t.Errorf("err=%v, want to wrap %v", err, listErr)
	}
}

func TestReconcile_EnsureInterfaceError(t *testing.T) {
	keyDir := t.TempDir()
	writeKey(t, keyDir, "ov", "key")
	ensureErr := errors.New("wg create failed")
	f := &fakeWG{ensureErr: ensureErr}
	r := newTestReconciler(f, keyDir)

	err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{overlay("ov")})
	if !errors.Is(err, ensureErr) {
		t.Errorf("err=%v, want to wrap %v", err, ensureErr)
	}
}

func TestReconcile_RemoveInterfaceError(t *testing.T) {
	removeErr := errors.New("wg remove failed")
	f := &fakeWG{
		interfaces: []wg.Interface{{Name: "pntfx-orphan"}},
		removeErr:  removeErr,
	}
	r := newTestReconciler(f, t.TempDir())

	err := r.Reconcile(context.Background(), nil)
	if !errors.Is(err, removeErr) {
		t.Errorf("err=%v, want to wrap %v", err, removeErr)
	}
}

// ── Reconcile: multiple overlays ─────────────────────────────────────────────

func TestReconcile_MultipleOverlays(t *testing.T) {
	keyDir := t.TempDir()
	writeKey(t, keyDir, "ov1", "key1")
	writeKey(t, keyDir, "ov2", "key2")

	f := &fakeWG{}
	r := newTestReconciler(f, keyDir)

	if err := r.Reconcile(context.Background(), []v1alpha1.NetworkOverlay{overlay("ov1"), overlay("ov2")}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.ensureCalls) != 2 {
		t.Errorf("ensureCalls=%d, want 2", len(f.ensureCalls))
	}
}
