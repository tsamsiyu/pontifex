package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v1alpha1 "github.com/tsamsiyu/pontifex/api/v1alpha1"
	opconfig "github.com/tsamsiyu/pontifex/apps/operator/internal/config"
)

// NetworkOverlayReconciler reconciles NetworkOverlay CRs into community
// allocation, per-overlay WG Secret, RBAC, gateway Deployments,
// per-internal-node Deployments, and resolved status.edges.
type NetworkOverlayReconciler struct {
	Client client.Client
	Scheme *runtime.Scheme
	Config opconfig.OperatorConfig
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

// Reconcile is a Phase 1 stub: it logs and returns.
func (r *NetworkOverlayReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("networkoverlay", req.Name)
	logger.Info("reconciling")
	return ctrl.Result{}, nil
}

// SetupWithManager wires the reconciler into the controller-runtime manager.
func (r *NetworkOverlayReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.NetworkOverlay{}).
		Complete(r)
}
