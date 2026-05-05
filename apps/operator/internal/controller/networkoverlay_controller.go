package controller

import (
	"context"
	"fmt"
	"reflect"
	"sort"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1alpha1 "github.com/tsamsiyu/pontifex/api/v1alpha1"
	"github.com/tsamsiyu/pontifex/apps/operator/internal/agent"
	opconfig "github.com/tsamsiyu/pontifex/apps/operator/internal/config"
	"github.com/tsamsiyu/pontifex/apps/operator/internal/wgkeys"
)

// rbacEnsurer is the function signature for ensuring agent RBAC, injectable
// so tests can substitute a no-op without modifying the agent package.
type rbacEnsurer func(ctx context.Context, c client.Client, namespace string) error

const finalizerName = "pontifex.io/networkoverlay"

// NetworkOverlayReconciler reconciles NetworkOverlay CRs into community
// allocation, per-overlay WG Secret, RBAC, gateway Deployments,
// per-internal-node Deployments, and resolved status.edges.
type NetworkOverlayReconciler struct {
	Client     client.Client
	Scheme     *runtime.Scheme
	Config     opconfig.OperatorConfig
	Keys       wgkeys.KeyPairEnsurer
	EnsureRBAC rbacEnsurer
}

// +kubebuilder:rbac:groups=pontifex.io,resources=networkoverlays,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=pontifex.io,resources=networkoverlays/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=pontifex.io,resources=networkoverlays/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=serviceaccounts;configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings,verbs=get;list;watch;create;update;patch;delete

func (r *NetworkOverlayReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("networkoverlay", req.Name)

	var overlay v1alpha1.NetworkOverlay
	if err := r.Client.Get(ctx, req.NamespacedName, &overlay); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !overlay.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, &overlay)
	}

	if !controllerutil.ContainsFinalizer(&overlay, finalizerName) {
		patch := client.MergeFrom(overlay.DeepCopy())
		controllerutil.AddFinalizer(&overlay, finalizerName)
		if err := r.Client.Patch(ctx, &overlay, patch); err != nil {
			return ctrl.Result{}, err
		}
	}

	var gwNodes corev1.NodeList
	if err := r.Client.List(ctx, &gwNodes, client.HasLabels{r.Config.GatewayNodeLabel}); err != nil {
		return ctrl.Result{}, err
	}
	gateways := buildGateways(gwNodes.Items, r.Config.GatewayNodeLabel)
	if !reflect.DeepEqual(overlay.Status.Gateways, gateways) {
		base := overlay.DeepCopy()
		overlay.Status.Gateways = gateways
		if err := r.Client.Status().Patch(ctx, &overlay, client.MergeFrom(base)); err != nil {
			return ctrl.Result{}, err
		}
	}

	if overlay.Status.Community == "" {
		comm, err := r.allocateCommunity(ctx, overlay.Name)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("allocate community: %w", err)
		}
		base := overlay.DeepCopy()
		overlay.Status.Community = comm
		if err := r.Client.Status().Patch(ctx, &overlay, client.MergeFrom(base)); err != nil {
			return ctrl.Result{}, err
		}
		logger.Info("allocated community", "community", comm)
	}

	pubKey, secretName, err := r.Keys.EnsureKeyPair(ctx, &overlay)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("ensure WG keypair: %w", err)
	}
	if overlay.Status.PublicKey != pubKey || overlay.Status.WGSecretRef == nil || overlay.Status.WGSecretRef.Name != secretName {
		base := overlay.DeepCopy()
		overlay.Status.PublicKey = pubKey
		overlay.Status.WGSecretRef = &corev1.SecretReference{Name: secretName, Namespace: r.Config.Namespace}
		if err := r.Client.Status().Patch(ctx, &overlay, client.MergeFrom(base)); err != nil {
			return ctrl.Result{}, err
		}
	}

	if err := r.EnsureRBAC(ctx, r.Client, r.Config.Namespace); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensure agent RBAC: %w", err)
	}

	var all v1alpha1.NetworkOverlayList
	if err := r.Client.List(ctx, &all); err != nil {
		return ctrl.Result{}, err
	}

	if err := agent.EnsureGatewayDeployments(ctx, r.Client, r.Config.Namespace, r.Config.AgentImage, r.Config.GatewayNodeLabel, all.Items); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensure gateway deployments: %w", err)
	}

	if err := agent.EnsureInternalDeployments(ctx, r.Client, r.Config.Namespace, r.Config.AgentImage, r.Config.GatewayNodeLabel, all.Items); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensure internal deployments: %w", err)
	}

	edges, err := r.resolveEdges(ctx, &overlay)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("resolve edges: %w", err)
	}
	if !reflect.DeepEqual(overlay.Status.Edges, edges) {
		base := overlay.DeepCopy()
		overlay.Status.Edges = edges
		if err := r.Client.Status().Patch(ctx, &overlay, client.MergeFrom(base)); err != nil {
			return ctrl.Result{}, err
		}
	}

	logger.Info("reconciled")
	return ctrl.Result{}, nil
}

func (r *NetworkOverlayReconciler) handleDeletion(ctx context.Context, overlay *v1alpha1.NetworkOverlay) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(overlay, finalizerName) {
		return ctrl.Result{}, nil
	}

	var all v1alpha1.NetworkOverlayList
	if err := r.Client.List(ctx, &all); err != nil {
		return ctrl.Result{}, err
	}
	remaining := make([]v1alpha1.NetworkOverlay, 0, len(all.Items))
	for _, o := range all.Items {
		if o.Name != overlay.Name {
			remaining = append(remaining, o)
		}
	}
	if err := agent.EnsureInternalDeployments(ctx, r.Client, r.Config.Namespace, r.Config.AgentImage, r.Config.GatewayNodeLabel, remaining); err != nil {
		return ctrl.Result{}, fmt.Errorf("teardown internal deployments: %w", err)
	}

	patch := client.MergeFrom(overlay.DeepCopy())
	controllerutil.RemoveFinalizer(overlay, finalizerName)
	return ctrl.Result{}, r.Client.Patch(ctx, overlay, patch)
}

func (r *NetworkOverlayReconciler) allocateCommunity(ctx context.Context, currentName string) (string, error) {
	var all v1alpha1.NetworkOverlayList
	if err := r.Client.List(ctx, &all); err != nil {
		return "", err
	}
	used := make(map[string]struct{}, len(all.Items))
	for _, o := range all.Items {
		if o.Name != currentName && o.Status.Community != "" {
			used[o.Status.Community] = struct{}{}
		}
	}
	for n := uint32(1); n < (1 << 16); n++ {
		c := fmt.Sprintf("%d:%d", r.Config.ASN, n)
		if _, ok := used[c]; !ok {
			return c, nil
		}
	}
	return "", fmt.Errorf("BGP community pool exhausted for ASN %d", r.Config.ASN)
}

func (r *NetworkOverlayReconciler) resolveEdges(ctx context.Context, overlay *v1alpha1.NetworkOverlay) ([]v1alpha1.EdgeStatus, error) {
	logger := log.FromContext(ctx).WithValues("networkoverlay", overlay.Name)

	var gatewayNodes corev1.NodeList
	if err := r.Client.List(ctx, &gatewayNodes, client.HasLabels{r.Config.GatewayNodeLabel}); err != nil {
		return nil, err
	}
	gatewaySet := make(map[string]struct{}, len(gatewayNodes.Items))
	for _, n := range gatewayNodes.Items {
		gatewaySet[n.Name] = struct{}{}
	}

	edges := make([]v1alpha1.EdgeStatus, 0, len(overlay.Spec.Edges))
	for _, edge := range overlay.Spec.Edges {
		status := v1alpha1.EdgeStatus{
			VirtualIP:    edge.VirtualIP,
			PodNamespace: edge.PodNamespace,
		}

		sel, err := labels.Parse(edge.PodLabelsSelector)
		if err != nil {
			logger.Error(err, "invalid podLabelsSelector", "selector", edge.PodLabelsSelector, "virtualIP", edge.VirtualIP)
			edges = append(edges, status)
			continue
		}

		var podList corev1.PodList
		if err := r.Client.List(ctx, &podList,
			client.InNamespace(edge.PodNamespace),
			client.MatchingLabelsSelector{Selector: sel},
		); err != nil {
			return nil, err
		}

		running := make([]corev1.Pod, 0, len(podList.Items))
		for i := range podList.Items {
			p := &podList.Items[i]
			if p.Status.Phase == corev1.PodRunning && p.DeletionTimestamp == nil {
				running = append(running, *p)
			}
		}

		switch len(running) {
		case 0:
			edges = append(edges, status)
			continue
		case 1:
			// handled below
		default:
			logger.Info("multiple pods match edge selector, skipping", "virtualIP", edge.VirtualIP, "count", len(running))
			edges = append(edges, status)
			continue
		}

		pod := running[0]
		nodeName := pod.Spec.NodeName
		if _, isGW := gatewaySet[nodeName]; isGW {
			logger.Info("edge resolves to gateway node, skipping", "virtualIP", edge.VirtualIP, "node", nodeName)
			edges = append(edges, status)
			continue
		}

		var node corev1.Node
		if err := r.Client.Get(ctx, client.ObjectKey{Name: nodeName}, &node); err != nil {
			return nil, err
		}
		var nodeIP string
		for _, addr := range node.Status.Addresses {
			if addr.Type == corev1.NodeInternalIP {
				nodeIP = addr.Address
				break
			}
		}

		status.PodName = pod.Name
		status.PodIP = pod.Status.PodIP
		status.NodeName = nodeName
		status.NodeIP = nodeIP
		status.Ready = true
		edges = append(edges, status)
	}

	return edges, nil
}

func buildGateways(nodes []corev1.Node, labelName string) []v1alpha1.Gateway {
	gateways := make([]v1alpha1.Gateway, 0, len(nodes))
	for _, node := range nodes {
		gw := v1alpha1.Gateway{
			NodeName:    node.Name,
			IsSecondary: node.Labels[labelName] == "secondary",
		}
		for _, addr := range node.Status.Addresses {
			switch addr.Type {
			case corev1.NodeInternalIP:
				gw.NodeIP = addr.Address
			case corev1.NodeExternalIP:
				gw.PublicIP = addr.Address
			}
		}
		gateways = append(gateways, gw)
	}
	sort.Slice(gateways, func(i, j int) bool {
		return gateways[i].NodeName < gateways[j].NodeName
	})
	return gateways
}

// SetupWithManager wires the reconciler into the controller-runtime manager.
func (r *NetworkOverlayReconciler) SetupWithManager(mgr ctrl.Manager) error {
	enqueueAll := handler.EnqueueRequestsFromMapFunc(
		func(ctx context.Context, _ client.Object) []reconcile.Request {
			var list v1alpha1.NetworkOverlayList
			if err := mgr.GetClient().List(ctx, &list); err != nil {
				return nil
			}
			reqs := make([]reconcile.Request, len(list.Items))
			for i := range list.Items {
				reqs[i].NamespacedName = client.ObjectKeyFromObject(&list.Items[i])
			}
			return reqs
		},
	)

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.NetworkOverlay{}).
		Watches(&corev1.Pod{}, enqueueAll).
		Watches(&corev1.Node{}, enqueueAll).
		Complete(r)
}
