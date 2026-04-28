package gateways

import (
	"context"
	"errors"

	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/tsamsiyu/pontifex/api/v1alpha1"
)

var errNotImplemented = errors.New("not implemented")

// Observer runs an informer over core/v1.Node filtered by the configured
// gateway-role label and broadcasts the current []v1alpha1.Gateway snapshot
// to subscribers on every relevant change.
type Observer struct {
	Client    client.Client
	LabelName string
}

// Subscribe returns a channel that receives the latest snapshot.
func (o *Observer) Subscribe() <-chan []v1alpha1.Gateway {
	ch := make(chan []v1alpha1.Gateway, 1)
	return ch
}

// Run starts the informer and blocks until ctx is done. Phase 1 stub.
func (o *Observer) Run(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

// Snapshot returns the latest []Gateway. Phase 1 stub returns an error.
func (o *Observer) Snapshot() ([]v1alpha1.Gateway, error) {
	return nil, errNotImplemented
}
