package deploy

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/tsamsiyu/pontifex/api/v1alpha1"
)

// EnsureInternalDeployments reconciles the per-internal-node agent
// Deployments. Target set is:
//
//	union(status.edges[].nodeName) over Ready=true edges, minus the set of
//	gateway-labeled nodes.
//
// Edges resolving to a gateway-labeled node are skipped (port conflicts on
// hostNetwork: true) and an Event is emitted on the NetworkOverlay.
//
// Phase 1 stub.
func EnsureInternalDeployments(
	ctx context.Context,
	c client.Client,
	namespace string,
	agentImage string,
	gatewayLabel string,
	overlays []v1alpha1.NetworkOverlay,
) error {
	return errNotImplemented
}
