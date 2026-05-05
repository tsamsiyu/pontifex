package agent

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	v1alpha1 "github.com/tsamsiyu/pontifex/api/v1alpha1"
)

// EnsureInternalDeployments reconciles the per-internal-node agent
// Deployments. Target set is:
//
//	union(status.edges[].nodeName) over Ready=true edges, minus the set of
//	gateway-labeled nodes.
func EnsureInternalDeployments(
	ctx context.Context,
	c client.Client,
	namespace string,
	agentImage string,
	gatewayLabel string,
	overlays []v1alpha1.NetworkOverlay,
) error {
	var gatewayNodes corev1.NodeList
	if err := c.List(ctx, &gatewayNodes, client.HasLabels{gatewayLabel}); err != nil {
		return err
	}
	gatewaySet := make(map[string]struct{}, len(gatewayNodes.Items))
	for _, n := range gatewayNodes.Items {
		gatewaySet[n.Name] = struct{}{}
	}

	targetNodes := make(map[string]struct{})
	for _, o := range overlays {
		for _, edge := range o.Status.Edges {
			if edge.Ready && edge.NodeName != "" {
				if _, isGW := gatewaySet[edge.NodeName]; !isGW {
					targetNodes[edge.NodeName] = struct{}{}
				}
			}
		}
	}

	var deplList appsv1.DeploymentList
	if err := c.List(ctx, &deplList,
		client.InNamespace(namespace),
		client.MatchingLabels{"app.kubernetes.io/component": "pontifex-internal"},
	); err != nil {
		return err
	}

	for i := range deplList.Items {
		d := &deplList.Items[i]
		nodeName := d.Labels["pontifex.io/node"]
		if _, ok := targetNodes[nodeName]; !ok {
			if err := c.Delete(ctx, d); client.IgnoreNotFound(err) != nil {
				return fmt.Errorf("delete internal deployment %s: %w", d.Name, err)
			}
		}
	}

	for nodeName := range targetNodes {
		replicas := int32(1)
		deplName := "pontifex-internal-" + nodeName

		podLabels := map[string]string{
			"app.kubernetes.io/name":      "pontifex-agent",
			"app.kubernetes.io/component": "pontifex-internal",
			"pontifex.io/node":            nodeName,
		}

		depl := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      deplName,
				Namespace: namespace,
			},
		}
		_, err := controllerutil.CreateOrUpdate(ctx, c, depl, func() error {
			depl.Labels = podLabels
			depl.Spec.Replicas = &replicas
			if depl.Spec.Selector == nil {
				depl.Spec.Selector = &metav1.LabelSelector{MatchLabels: podLabels}
			}
			depl.Spec.Template.Labels = podLabels
			depl.Spec.Template.Spec.HostNetwork = true
			depl.Spec.Template.Spec.Affinity = nodeNameAffinity(nodeName)
			depl.Spec.Template.Spec.Containers = []corev1.Container{
				{
					Name:  "agent",
					Image: agentImage,
					Args:  []string{"--mode=internal"},
					SecurityContext: &corev1.SecurityContext{
						Capabilities: &corev1.Capabilities{
							Add: []corev1.Capability{"NET_ADMIN", "NET_RAW", "SYS_ADMIN"},
						},
					},
				},
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("create/update internal deployment %s: %w", deplName, err)
		}
	}
	return nil
}
