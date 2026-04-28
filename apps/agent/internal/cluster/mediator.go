package cluster

import (
	"context"

	"go.uber.org/zap"

	v1alpha1 "github.com/tsamsiyu/pontifex/api/v1alpha1"
)

// Mediator consumes Observer events, debounces them, and on each tick fans
// out the current []NetworkOverlay snapshot to all subscribers. It is
// deliberately thin: no business logic, no diffing, no error tracking.
type Mediator interface {
	Subscribe() <-chan []v1alpha1.NetworkOverlay
}

// mediator is the concrete impl. Phase 1 stub: subscribers are tracked but
// nothing is ever sent on them.
type mediator struct {
	logger      *zap.Logger
	observer    *Observer
	subscribers []chan []v1alpha1.NetworkOverlay
}

// NewMediator returns a Mediator wrapping the given Observer.
func NewMediator(observer *Observer, logger *zap.Logger) Mediator {
	return &mediator{
		logger:   logger,
		observer: observer,
	}
}

// Subscribe returns a fresh channel; the mediator delivers the current
// snapshot to every subscriber on each debounced tick.
func (m *mediator) Subscribe() <-chan []v1alpha1.NetworkOverlay {
	ch := make(chan []v1alpha1.NetworkOverlay, 1)
	m.subscribers = append(m.subscribers, ch)
	return ch
}

// Run consumes observer events and broadcasts snapshots to subscribers.
// Phase 1 stub.
func (m *mediator) Run(ctx context.Context) error {
	<-ctx.Done()
	return nil
}
