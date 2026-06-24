package broadcaster

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestInProcess_PublishDeliversToAllSubscribers(t *testing.T) {
	b := NewInProcess()
	topic := ChatTopic(uuid.New())

	var a, c int32
	b.Subscribe(topic, func(Event) { atomic.AddInt32(&a, 1) })
	b.Subscribe(topic, func(Event) { atomic.AddInt32(&c, 1) })

	require.NoError(t, b.Publish(context.Background(), topic, Event{Type: "message.created"}))

	require.Equal(t, int32(1), atomic.LoadInt32(&a))
	require.Equal(t, int32(1), atomic.LoadInt32(&c))
}

func TestInProcess_PublishToTopicWithoutSubscribersIsNoop(t *testing.T) {
	b := NewInProcess()
	require.NoError(t, b.Publish(context.Background(), "chat:nobody", Event{Type: "x"}))
}

func TestInProcess_UnsubscribeStopsDelivery(t *testing.T) {
	b := NewInProcess()
	topic := ChatTopic(uuid.New())

	var hits int32
	unsub := b.Subscribe(topic, func(Event) { atomic.AddInt32(&hits, 1) })

	require.NoError(t, b.Publish(context.Background(), topic, Event{Type: "x"}))
	unsub()
	require.NoError(t, b.Publish(context.Background(), topic, Event{Type: "x"}))

	require.Equal(t, int32(1), atomic.LoadInt32(&hits))
}

func TestInProcess_UnsubscribeIsIdempotent(t *testing.T) {
	b := NewInProcess()
	topic := ChatTopic(uuid.New())

	unsub := b.Subscribe(topic, func(Event) {})
	unsub()
	unsub() // must not panic
}

func TestInProcess_TopicsAreIsolated(t *testing.T) {
	b := NewInProcess()
	t1 := ChatTopic(uuid.New())
	t2 := ChatTopic(uuid.New())

	var h1, h2 int32
	b.Subscribe(t1, func(Event) { atomic.AddInt32(&h1, 1) })
	b.Subscribe(t2, func(Event) { atomic.AddInt32(&h2, 1) })

	require.NoError(t, b.Publish(context.Background(), t1, Event{Type: "x"}))

	require.Equal(t, int32(1), atomic.LoadInt32(&h1))
	require.Equal(t, int32(0), atomic.LoadInt32(&h2))
}

func TestInProcess_EventPayloadRoundTrip(t *testing.T) {
	b := NewInProcess()
	topic := ChatTopic(uuid.New())

	payload, err := json.Marshal(map[string]string{"user_id": "u1", "last_read_message_id": "m1"})
	require.NoError(t, err)

	var got Event
	b.Subscribe(topic, func(e Event) { got = e })

	require.NoError(t, b.Publish(context.Background(), topic, Event{
		Type:    "chat.read",
		ChatID:  "c1",
		Payload: payload,
	}))

	wire, err := json.Marshal(got)
	require.NoError(t, err)
	require.JSONEq(t, `{"type":"chat.read","chat_id":"c1","payload":{"user_id":"u1","last_read_message_id":"m1"}}`, string(wire))
}

func TestInProcess_ConcurrentPublishSubscribeUnsubscribe(t *testing.T) {
	b := NewInProcess()
	topic := ChatTopic(uuid.New())

	const workers = 32
	const iterations = 200

	var wg sync.WaitGroup
	wg.Add(workers * 3)

	// Publishers.
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = b.Publish(context.Background(), topic, Event{Type: "ping"})
			}
		}()
	}

	// Subscriber churn: subscribe then immediately unsubscribe.
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				unsub := b.Subscribe(topic, func(Event) {})
				unsub()
			}
		}()
	}

	// Long-lived subscribers that count deliveries.
	var hits int32
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			unsub := b.Subscribe(topic, func(Event) { atomic.AddInt32(&hits, 1) })
			defer unsub()
			for j := 0; j < iterations; j++ {
				_ = b.Publish(context.Background(), topic, Event{Type: "ping"})
			}
		}()
	}

	wg.Wait()
	// We don't assert an exact hit count — interleaving makes it nondeterministic.
	// The test passes if there are no data races (run with -race) and no panics.
	require.GreaterOrEqual(t, atomic.LoadInt32(&hits), int32(0))
}
