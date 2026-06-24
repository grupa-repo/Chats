package broadcaster

import (
	"context"
	"sync"
	"sync/atomic"
)

// InProcess is a single-replica Broadcaster. Subscribers are kept in memory
// and notified synchronously inside Publish. This is intentional: WebSocket
// connections own the buffered outbound channel and drop/close on overflow,
// so the broadcaster itself stays simple.
//
// When the chat service scales beyond one replica, swap this for a
// bus-backed implementation that satisfies the same interface.
type InProcess struct {
	mu     sync.RWMutex
	nextID uint64
	topics map[string]map[uint64]Handler
}

// NewInProcess returns a ready-to-use in-process broadcaster.
func NewInProcess() *InProcess {
	return &InProcess{
		topics: make(map[string]map[uint64]Handler),
	}
}

// Publish invokes every handler registered for topic with the given event.
// Handlers are called under an RLock — they must not block.
func (b *InProcess) Publish(_ context.Context, topic string, event Event) error {
	b.mu.RLock()
	subs := b.topics[topic]
	handlers := make([]Handler, 0, len(subs))
	for _, h := range subs {
		handlers = append(handlers, h)
	}
	b.mu.RUnlock()

	for _, h := range handlers {
		h(event)
	}
	return nil
}

// Subscribe registers handler for topic. The returned function removes this
// subscription and is safe to call multiple times.
func (b *InProcess) Subscribe(topic string, handler Handler) func() {
	id := atomic.AddUint64(&b.nextID, 1)

	b.mu.Lock()
	if b.topics[topic] == nil {
		b.topics[topic] = make(map[uint64]Handler)
	}
	b.topics[topic][id] = handler
	b.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			if subs, ok := b.topics[topic]; ok {
				delete(subs, id)
				if len(subs) == 0 {
					delete(b.topics, topic)
				}
			}
		})
	}
}
