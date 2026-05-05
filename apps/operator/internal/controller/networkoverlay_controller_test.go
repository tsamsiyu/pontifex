package controller

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/tsamsiyu/pontifex/api/v1alpha1"
	opconfig "github.com/tsamsiyu/pontifex/apps/operator/internal/config"
	"github.com/tsamsiyu/pontifex/apps/operator/internal/wgkeys"
)

// ── helpers ──────────────────────────────────────────────────────────────────

func testScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = v1alpha1.AddToScheme(s)
	return s
}

func defaultCfg() opconfig.OperatorConfig {
	return opconfig.OperatorConfig{
		Namespace:        "pontifex-system",
		ASN:              65000,
		AgentImage:       "agent:test",
		GatewayNodeLabel: "pontifex.io/gateway-role",
	}
}

type noopKeys struct{}

func (noopKeys) EnsureKeyPair(_ context.Context, _ *v1alpha1.NetworkOverlay) (string, string, error) {
	return "pubkey", "secret-name", nil
}

var _ wgkeys.KeyPairEnsurer = noopKeys{}

func newReconciler(c client.Client) *NetworkOverlayReconciler {
	return &NetworkOverlayReconciler{
		Client:     c,
		Scheme:     testScheme(),
		Config:     defaultCfg(),
		Keys:       noopKeys{},
		EnsureRBAC: func(_ context.Context, _ client.Client, _ string) error { return nil },
	}
}

func fakeClient(objs ...client.Object) client.Client {
	return fake.NewClientBuilder().
		WithScheme(testScheme()).
		WithObjects(objs...).
		WithStatusSubresource(&v1alpha1.NetworkOverlay{}).
		Build()
}

func newOverlay(name string) *v1alpha1.NetworkOverlay {
	return &v1alpha1.NetworkOverlay{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       v1alpha1.NetworkOverlaySpec{VirtualCIDR: "10.100.0.0/16"},
	}
}

func newGatewayNode(name, roleValue string, internalIP, externalIP string) *corev1.Node {
	n := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{"pontifex.io/gateway-role": roleValue},
		},
	}
	if internalIP != "" {
		n.Status.Addresses = append(n.Status.Addresses, corev1.NodeAddress{Type: corev1.NodeInternalIP, Address: internalIP})
	}
	if externalIP != "" {
		n.Status.Addresses = append(n.Status.Addresses, corev1.NodeAddress{Type: corev1.NodeExternalIP, Address: externalIP})
	}
	return n
}

func newWorkerNode(name, internalIP string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{
				{Type: corev1.NodeInternalIP, Address: internalIP},
			},
		},
	}
}

func reconcileOnce(t *testing.T, r *NetworkOverlayReconciler, name string) ctrl.Result {
	t.Helper()
	res, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: name},
	})
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	return res
}

func getOverlay(t *testing.T, c client.Client, name string) v1alpha1.NetworkOverlay {
	t.Helper()
	var o v1alpha1.NetworkOverlay
	if err := c.Get(context.Background(), types.NamespacedName{Name: name}, &o); err != nil {
		t.Fatalf("get overlay %q: %v", name, err)
	}
	return o
}

// ── buildGateways ─────────────────────────────────────────────────────────────

func TestBuildGateways_Empty(t *testing.T) {
	gws := buildGateways(nil, "pontifex.io/gateway-role")
	if len(gws) != 0 {
		t.Fatalf("expected empty slice, got %v", gws)
	}
}

func TestBuildGateways_Primary(t *testing.T) {
	node := newGatewayNode("gw1", "primary", "10.0.0.1", "")
	gws := buildGateways([]corev1.Node{*node}, "pontifex.io/gateway-role")
	if len(gws) != 1 {
		t.Fatalf("expected 1 gateway, got %d", len(gws))
	}
	g := gws[0]
	if g.IsSecondary {
		t.Error("expected IsSecondary=false for primary")
	}
	if g.NodeIP != "10.0.0.1" {
		t.Errorf("NodeIP=%q, want 10.0.0.1", g.NodeIP)
	}
}

func TestBuildGateways_Secondary(t *testing.T) {
	node := newGatewayNode("gw2", "secondary", "10.0.0.2", "")
	gws := buildGateways([]corev1.Node{*node}, "pontifex.io/gateway-role")
	if !gws[0].IsSecondary {
		t.Error("expected IsSecondary=true for secondary")
	}
}

func TestBuildGateways_PublicIP(t *testing.T) {
	node := newGatewayNode("gw1", "primary", "10.0.0.1", "1.2.3.4")
	gws := buildGateways([]corev1.Node{*node}, "pontifex.io/gateway-role")
	if gws[0].PublicIP != "1.2.3.4" {
		t.Errorf("PublicIP=%q, want 1.2.3.4", gws[0].PublicIP)
	}
}

func TestBuildGateways_Sorted(t *testing.T) {
	nodeZ := newGatewayNode("z-node", "primary", "10.0.0.3", "")
	nodeA := newGatewayNode("a-node", "primary", "10.0.0.1", "")
	gws := buildGateways([]corev1.Node{*nodeZ, *nodeA}, "pontifex.io/gateway-role")
	if gws[0].NodeName != "a-node" || gws[1].NodeName != "z-node" {
		t.Errorf("expected sorted [a-node, z-node], got [%s, %s]", gws[0].NodeName, gws[1].NodeName)
	}
}

// ── allocateCommunity ─────────────────────────────────────────────────────────

func TestAllocateCommunity_First(t *testing.T) {
	c := fakeClient()
	r := newReconciler(c)
	comm, err := r.allocateCommunity(context.Background(), "overlay-a")
	if err != nil {
		t.Fatal(err)
	}
	if comm != "65000:1" {
		t.Errorf("community=%q, want 65000:1", comm)
	}
}

func TestAllocateCommunity_SkipsUsed(t *testing.T) {
	existing := newOverlay("overlay-b")
	existing.Status.Community = "65000:1"
	c := fakeClient(existing)
	if err := c.Status().Update(context.Background(), existing); err != nil {
		t.Fatal(err)
	}
	r := newReconciler(c)
	comm, err := r.allocateCommunity(context.Background(), "overlay-a")
	if err != nil {
		t.Fatal(err)
	}
	if comm != "65000:2" {
		t.Errorf("community=%q, want 65000:2", comm)
	}
}

func TestAllocateCommunity_SkipsSelf(t *testing.T) {
	self := newOverlay("overlay-a")
	self.Status.Community = "65000:1"
	c := fakeClient(self)
	if err := c.Status().Update(context.Background(), self); err != nil {
		t.Fatal(err)
	}
	r := newReconciler(c)
	comm, err := r.allocateCommunity(context.Background(), "overlay-a")
	if err != nil {
		t.Fatal(err)
	}
	// self is excluded from the used set, so 65000:1 is still available
	if comm != "65000:1" {
		t.Errorf("community=%q, want 65000:1", comm)
	}
}

// ── resolveEdges ──────────────────────────────────────────────────────────────

func makePod(name, namespace, nodeName, podIP string, phase corev1.PodPhase, labels map[string]string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec:   corev1.PodSpec{NodeName: nodeName},
		Status: corev1.PodStatus{Phase: phase, PodIP: podIP},
	}
}

func overlayWithEdge(name, podNS, selector, vip string) *v1alpha1.NetworkOverlay {
	o := newOverlay(name)
	o.Spec.Edges = []v1alpha1.Edge{
		{PodNamespace: podNS, PodLabelsSelector: selector, VirtualIP: vip},
	}
	return o
}

func TestResolveEdges_NoPods(t *testing.T) {
	overlay := overlayWithEdge("ov", "default", "app=foo", "10.100.0.1")
	c := fakeClient()
	r := newReconciler(c)
	edges, err := r.resolveEdges(context.Background(), overlay)
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 1 || edges[0].Ready {
		t.Errorf("expected 1 unready edge, got %+v", edges)
	}
}

func TestResolveEdges_Ready(t *testing.T) {
	worker := newWorkerNode("worker1", "192.168.1.10")
	pod := makePod("mypod", "default", "worker1", "10.0.1.5", corev1.PodRunning, map[string]string{"app": "foo"})
	overlay := overlayWithEdge("ov", "default", "app=foo", "10.100.0.1")
	c := fakeClient(worker, pod)
	r := newReconciler(c)
	edges, err := r.resolveEdges(context.Background(), overlay)
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(edges))
	}
	e := edges[0]
	if !e.Ready {
		t.Error("expected Ready=true")
	}
	if e.PodName != "mypod" || e.PodIP != "10.0.1.5" || e.NodeName != "worker1" || e.NodeIP != "192.168.1.10" {
		t.Errorf("unexpected edge values: %+v", e)
	}
}

func TestResolveEdges_MultiplePods(t *testing.T) {
	worker := newWorkerNode("worker1", "192.168.1.10")
	pod1 := makePod("pod1", "default", "worker1", "10.0.1.5", corev1.PodRunning, map[string]string{"app": "foo"})
	pod2 := makePod("pod2", "default", "worker1", "10.0.1.6", corev1.PodRunning, map[string]string{"app": "foo"})
	overlay := overlayWithEdge("ov", "default", "app=foo", "10.100.0.1")
	c := fakeClient(worker, pod1, pod2)
	r := newReconciler(c)
	edges, err := r.resolveEdges(context.Background(), overlay)
	if err != nil {
		t.Fatal(err)
	}
	if edges[0].Ready {
		t.Error("expected Ready=false for ambiguous multi-pod match")
	}
}

func TestResolveEdges_GatewayNode(t *testing.T) {
	gwNode := newGatewayNode("gw1", "primary", "10.0.0.1", "")
	pod := makePod("mypod", "default", "gw1", "10.0.1.5", corev1.PodRunning, map[string]string{"app": "foo"})
	overlay := overlayWithEdge("ov", "default", "app=foo", "10.100.0.1")
	c := fakeClient(gwNode, pod)
	r := newReconciler(c)
	edges, err := r.resolveEdges(context.Background(), overlay)
	if err != nil {
		t.Fatal(err)
	}
	if edges[0].Ready {
		t.Error("expected Ready=false when pod is on gateway node")
	}
}

func TestResolveEdges_InvalidSelector(t *testing.T) {
	overlay := overlayWithEdge("ov", "default", "!!invalid", "10.100.0.1")
	c := fakeClient()
	r := newReconciler(c)
	edges, err := r.resolveEdges(context.Background(), overlay)
	if err != nil {
		t.Fatalf("expected no error for invalid selector, got: %v", err)
	}
	if len(edges) != 1 || edges[0].Ready {
		t.Errorf("expected 1 unready edge, got %+v", edges)
	}
}

func TestResolveEdges_NonRunningPod(t *testing.T) {
	worker := newWorkerNode("worker1", "192.168.1.10")
	pod := makePod("mypod", "default", "worker1", "10.0.1.5", corev1.PodPending, map[string]string{"app": "foo"})
	overlay := overlayWithEdge("ov", "default", "app=foo", "10.100.0.1")
	c := fakeClient(worker, pod)
	r := newReconciler(c)
	edges, err := r.resolveEdges(context.Background(), overlay)
	if err != nil {
		t.Fatal(err)
	}
	if edges[0].Ready {
		t.Error("expected Ready=false for pending pod")
	}
}

// ── Reconcile ─────────────────────────────────────────────────────────────────

func TestReconcile_NotFound(t *testing.T) {
	c := fakeClient()
	r := newReconciler(c)
	res, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "does-not-exist"},
	})
	if err != nil {
		t.Fatalf("expected nil error for not-found, got: %v", err)
	}
	if res.Requeue {
		t.Error("expected no requeue for not-found")
	}
}

func TestReconcile_AddsFinalizer(t *testing.T) {
	overlay := newOverlay("ov1")
	c := fakeClient(overlay)
	r := newReconciler(c)
	reconcileOnce(t, r, "ov1")
	got := getOverlay(t, c, "ov1")
	found := false
	for _, f := range got.Finalizers {
		if f == finalizerName {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("finalizer %q not added; finalizers: %v", finalizerName, got.Finalizers)
	}
}

func TestReconcile_AllocatesCommunity(t *testing.T) {
	overlay := newOverlay("ov1")
	c := fakeClient(overlay)
	r := newReconciler(c)
	reconcileOnce(t, r, "ov1")
	got := getOverlay(t, c, "ov1")
	if got.Status.Community == "" {
		t.Error("expected community to be allocated")
	}
	if got.Status.Community != "65000:1" {
		t.Errorf("community=%q, want 65000:1", got.Status.Community)
	}
}

func TestReconcile_SkipsAllocatedCommunity(t *testing.T) {
	overlay := newOverlay("ov1")
	c := fakeClient(overlay)
	r := newReconciler(c)
	// first reconcile allocates
	reconcileOnce(t, r, "ov1")
	got := getOverlay(t, c, "ov1")
	original := got.Status.Community
	// second reconcile should not change it
	reconcileOnce(t, r, "ov1")
	got2 := getOverlay(t, c, "ov1")
	if got2.Status.Community != original {
		t.Errorf("community changed on second reconcile: %q → %q", original, got2.Status.Community)
	}
}

func TestReconcile_GatewayStatus(t *testing.T) {
	overlay := newOverlay("ov1")
	gwNode := newGatewayNode("gw1", "primary", "10.0.0.1", "")
	c := fakeClient(overlay, gwNode)
	r := newReconciler(c)
	reconcileOnce(t, r, "ov1")
	got := getOverlay(t, c, "ov1")
	if len(got.Status.Gateways) != 1 {
		t.Fatalf("expected 1 gateway in status, got %d", len(got.Status.Gateways))
	}
	gw := got.Status.Gateways[0]
	if gw.NodeName != "gw1" || gw.NodeIP != "10.0.0.1" || gw.IsSecondary {
		t.Errorf("unexpected gateway: %+v", gw)
	}
}

func TestReconcile_CreatesGatewayDeployments(t *testing.T) {
	overlay := newOverlay("ov1")
	gwNode := newGatewayNode("gw1", "primary", "10.0.0.1", "")
	c := fakeClient(overlay, gwNode)
	r := newReconciler(c)
	reconcileOnce(t, r, "ov1")

	var depList appsv1.DeploymentList
	if err := c.List(context.Background(), &depList, client.InNamespace("pontifex-system")); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range depList.Items {
		if d.Name == "pontifex-gateway-primary" {
			found = true
			break
		}
	}
	if !found {
		names := make([]string, len(depList.Items))
		for i, d := range depList.Items {
			names[i] = d.Name
		}
		t.Errorf("pontifex-gateway-primary deployment not found; deployments: %v", names)
	}
}

func TestReconcile_Deletion(t *testing.T) {
	now := metav1.NewTime(time.Now())
	overlay := newOverlay("ov1")
	overlay.Finalizers = []string{finalizerName}
	overlay.DeletionTimestamp = &now
	c := fakeClient(overlay)
	r := newReconciler(c)

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "ov1"},
	})
	if err != nil {
		t.Fatalf("Reconcile during deletion returned error: %v", err)
	}

	// After finalizer removal the fake client GCs the object, so either
	// not-found (GC'd) or found-without-finalizer are both correct outcomes.
	var got v1alpha1.NetworkOverlay
	getErr := c.Get(context.Background(), types.NamespacedName{Name: "ov1"}, &got)
	if getErr == nil {
		for _, f := range got.Finalizers {
			if f == finalizerName {
				t.Errorf("finalizer %q was not removed after deletion", finalizerName)
			}
		}
	}
}
