package deploy

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/tsamsiyu/pontifex/api/v1alpha1"
)

// EnsureGatewayDeployments ensures the two single-replica gateway Deployments
// (primary, secondary) exist in the operator namespace, each pinned to the
// lex-smallest node carrying the matching gateway-role label value. With no
// labeled node for a role, the corresponding Deployment is created with
// replicas: 0.
//
// Each Deployment mounts every overlay's WG Secret at
// /etc/pontifex/wg/<overlay>/private and runs the agent with --mode=gateway.
//
// Phase 1 stub.
func EnsureGatewayDeployments(
	ctx context.Context,
	c client.Client,
	namespace string,
	agentImage string,
	gatewayLabel string,
	overlays []v1alpha1.NetworkOverlay,
) error {
	return errNotImplemented
}
