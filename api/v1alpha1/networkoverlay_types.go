package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NetworkOverlaySpec describes the desired topology of a single overlay.
type NetworkOverlaySpec struct {
	// VirtualCIDR is the pool from which edge virtual IPs are drawn.
	VirtualCIDR string `json:"virtualCIDR"`

	// Peers are external clusters reachable over WireGuard.
	Peers []Peer `json:"peers,omitempty"`

	// Edges declare local pods that should be reachable from peers via a
	// stable virtual IP.
	Edges []Edge `json:"edges,omitempty"`
}

// Peer is an external cluster participating in this overlay.
type Peer struct {
	Name       string   `json:"name"`
	PublicKey  string   `json:"publicKey"`
	Endpoint   string   `json:"endpoint"`
	AllowedIPs []string `json:"allowedIPs"`
	ASN        uint32   `json:"asn"`
}

// Edge selects a local pod that should be exposed to peers under a virtual IP.
type Edge struct {
	PodNamespace string `json:"podNamespace"`
	// PodLabelsSelector is a label-selector string parsed via
	// k8s.io/apimachinery/pkg/labels.Parse (e.g. "app=foo,tier in (web,api)").
	PodLabelsSelector string `json:"podLabelsSelector"`
	VirtualIP         string `json:"virtualIP"`
}

// NetworkOverlayStatus is operator-managed.
type NetworkOverlayStatus struct {
	// Community is the operator-allocated BGP community for this overlay,
	// formatted as "<clusterASN>:<n>".
	Community string `json:"community,omitempty"`

	// PublicKey is the WireGuard public key for this overlay, exposed so
	// peers can configure their side.
	PublicKey string `json:"publicKey,omitempty"`

	// WGSecretRef references the Secret holding the per-overlay WireGuard
	// private key, mounted into gateway pods by the operator.
	WGSecretRef *corev1.SecretReference `json:"wgSecretRef,omitempty"`

	// Gateways are the operator-discovered gateway nodes carrying the
	// configured gateway-role label.
	Gateways []Gateway `json:"gateways,omitempty"`

	// Edges mirror spec.edges with operator-resolved pod placement.
	Edges []EdgeStatus `json:"edges,omitempty"`
}

// Gateway is a node selected as a gateway for this overlay.
type Gateway struct {
	NodeName    string `json:"nodeName"`
	NodeIP      string `json:"nodeIP"`
	PublicIP    string `json:"publicIP,omitempty"`
	IsSecondary bool   `json:"isSecondary"`
}

// EdgeStatus reports the resolved pod for one Edge.
type EdgeStatus struct {
	VirtualIP    string `json:"virtualIP"`
	PodNamespace string `json:"podNamespace"`
	PodName      string `json:"podName,omitempty"`
	PodIP        string `json:"podIP,omitempty"`
	NodeName     string `json:"nodeName,omitempty"`
	NodeIP       string `json:"nodeIP,omitempty"`
	Ready        bool   `json:"ready"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=no
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Community",type=string,JSONPath=`.status.community`
// +kubebuilder:printcolumn:name="Edges",type=integer,JSONPath=`.spec.edges[*]`,priority=1
// +kubebuilder:printcolumn:name="Peers",type=integer,JSONPath=`.spec.peers[*]`,priority=1
// +kubebuilder:printcolumn:name="Gateways",type=integer,JSONPath=`.status.gateways[*]`,priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// NetworkOverlay declares an overlay topology between this cluster and
// peer clusters.
type NetworkOverlay struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NetworkOverlaySpec   `json:"spec,omitempty"`
	Status NetworkOverlayStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// NetworkOverlayList is a list of NetworkOverlay.
type NetworkOverlayList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NetworkOverlay `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NetworkOverlay{}, &NetworkOverlayList{})
}
