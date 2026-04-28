package resolver

import (
	"context"
	"errors"

	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/tsamsiyu/pontifex/api/v1alpha1"
)

var errNotImplemented = errors.New("not implemented")

// Resolver watches Pods and Nodes, parses spec.edges[].podLabelsSelector via
// labels.Parse, and produces a []EdgeStatus for the controller to write into
// status.edges.
type Resolver struct {
	Client           client.Client
	GatewayNodeLabel string
}

// Resolve returns []EdgeStatus for the given overlay. Phase 1 stub.
func (r *Resolver) Resolve(ctx context.Context, overlay *v1alpha1.NetworkOverlay) ([]v1alpha1.EdgeStatus, error) {
	return nil, errNotImplemented
}
