package cluster

import (
	"context"

	"go.uber.org/zap"

	v1alpha1 "github.com/tsamsiyu/pontifex/api/v1alpha1"
)

// Event is a raw informer event for a NetworkOverlay.
type Event struct {
	Type   EventType
	Object *v1alpha1.NetworkOverlay
}

// EventType classifies an Event.
type EventType int

const (
	EventAdd EventType = iota
	EventUpdate
	EventDelete
)

// Observer wraps the controller-runtime cache for NetworkOverlay and emits
// raw Add/Update/Delete events on its output channel.
//
// Phase 1: skeleton. Run blocks until ctx is done; no events are produced.
type Observer struct {
	logger *zap.Logger
	out    chan Event
}

// NewObserver returns an Observer.
func NewObserver(logger *zap.Logger) *Observer {
	return &Observer{
		logger: logger,
		out:    make(chan Event, 32),
	}
}

// Events returns the observer's event channel.
func (o *Observer) Events() <-chan Event {
	return o.out
}

// Run starts the observer. Phase 1 stub.
func (o *Observer) Run(ctx context.Context) error {
	<-ctx.Done()
	return nil
}
