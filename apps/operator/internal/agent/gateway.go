package agent

import (
	"context"
	"fmt"
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

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
func EnsureGatewayDeployments(
	ctx context.Context,
	c client.Client,
	namespace string,
	agentImage string,
	gatewayLabel string,
	overlays []v1alpha1.NetworkOverlay,
) error {
	logger := log.FromContext(ctx)

	var nodeList corev1.NodeList
	if err := c.List(ctx, &nodeList, client.HasLabels{gatewayLabel}); err != nil {
		return err
	}

	byRole := map[string][]corev1.Node{}
	for _, n := range nodeList.Items {
		role := n.Labels[gatewayLabel]
		byRole[role] = append(byRole[role], n)
	}
	for role := range byRole {
		nodes := byRole[role]
		sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
		byRole[role] = nodes
		if len(nodes) > 1 {
			logger.Info("multiple nodes with gateway role, using lex-smallest", "role", role, "chosen", nodes[0].Name)
		}
	}

	for _, role := range []string{"primary", "secondary"} {
		isSecondary := role == "secondary"
		deplName := "pontifex-gateway-" + role

		replicas := int32(0)
		chosenNode := ""
		if nodes := byRole[role]; len(nodes) > 0 {
			replicas = 1
			chosenNode = nodes[0].Name
		}

		volumes, mounts := buildWGVolumes(overlays)

		podLabels := map[string]string{
			"app.kubernetes.io/name":      "pontifex-agent",
			"app.kubernetes.io/component": "gateway",
			"pontifex.io/role":            role,
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
			if chosenNode != "" {
				depl.Spec.Template.Spec.Affinity = nodeNameAffinity(chosenNode)
			} else {
				depl.Spec.Template.Spec.Affinity = nil
			}
			depl.Spec.Template.Spec.Volumes = volumes
			depl.Spec.Template.Spec.Containers = []corev1.Container{
				{
					Name:  "agent",
					Image: agentImage,
					Args:  []string{"--mode=gateway"},
					Env: []corev1.EnvVar{
						{Name: "IS_SECONDARY", Value: fmt.Sprintf("%v", isSecondary)},
					},
					SecurityContext: &corev1.SecurityContext{
						Capabilities: &corev1.Capabilities{
							Add: []corev1.Capability{"NET_ADMIN", "NET_RAW", "SYS_ADMIN"},
						},
					},
					VolumeMounts: mounts,
				},
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("create/update gateway deployment %s: %w", deplName, err)
		}
	}
	return nil
}

func buildWGVolumes(overlays []v1alpha1.NetworkOverlay) ([]corev1.Volume, []corev1.VolumeMount) {
	volumes := make([]corev1.Volume, 0, len(overlays))
	mounts := make([]corev1.VolumeMount, 0, len(overlays))
	for _, o := range overlays {
		if o.Status.WGSecretRef == nil {
			continue
		}
		volName := "wg-" + o.Name
		volumes = append(volumes, corev1.Volume{
			Name: volName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: o.Status.WGSecretRef.Name,
				},
			},
		})
		mounts = append(mounts, corev1.VolumeMount{
			Name:      volName,
			MountPath: "/etc/pontifex/wg/" + o.Name + "/private",
			SubPath:   "private",
			ReadOnly:  true,
		})
	}
	return volumes, mounts
}

func nodeNameAffinity(nodeName string) *corev1.Affinity {
	return &corev1.Affinity{
		NodeAffinity: &corev1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{
					{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{
								Key:      "kubernetes.io/hostname",
								Operator: corev1.NodeSelectorOpIn,
								Values:   []string{nodeName},
							},
						},
					},
				},
			},
		},
	}
}
