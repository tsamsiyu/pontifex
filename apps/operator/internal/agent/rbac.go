package agent

import (
	"context"
	"errors"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

var errNotImplemented = errors.New("not implemented")

// EnsureAgentRBAC creates the cluster-scoped ServiceAccount, ClusterRole, and
// ClusterRoleBinding granting the agent only get/list/watch on
// NetworkOverlay. The agent never writes back; all spec/status updates go
// through the operator's own SA. Owner-refs target the sentinel ConfigMap so
// these resources are GC'd when the operator is uninstalled.
func EnsureAgentRBAC(ctx context.Context, c client.Client, namespace string) error {
	return errNotImplemented
}
