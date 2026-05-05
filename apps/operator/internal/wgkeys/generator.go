package wgkeys

import (
	"context"
	"errors"

	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/tsamsiyu/pontifex/api/v1alpha1"
)

var errNotImplemented = errors.New("not implemented")

// KeyPairEnsurer is the interface the reconciler uses to obtain a per-overlay
// WireGuard keypair. *Generator satisfies it; tests may substitute a no-op.
type KeyPairEnsurer interface {
	EnsureKeyPair(ctx context.Context, overlay *v1alpha1.NetworkOverlay) (publicKey, secretName string, err error)
}

// Generator creates the per-overlay WireGuard keypair on first reconcile,
// stores the private key in a namespaced Secret (one Secret per overlay), and
// returns the public key for status.publicKey.
type Generator struct {
	Client    client.Client
	Namespace string
}

// EnsureKeyPair returns the public key and a SecretReference. If the Secret
// does not yet exist, it is created with an owner-ref to the NetworkOverlay.
func (g *Generator) EnsureKeyPair(ctx context.Context, overlay *v1alpha1.NetworkOverlay) (publicKey string, secretName string, err error) {
	return "", "", errNotImplemented
}
