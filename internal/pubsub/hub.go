package pubsub

import (
	"sync"
	"sync/atomic"
)

const DefaultSubscriberBuffer = 256

type Message struct {
	Channel string
	Payload []byte
}

type Subscriber struct {
	ID       uint64
	Messages chan Message

	mu       sync.RWMutex
	channels map[string]struct{}
}

type Hub struct {
	mu          sync.RWMutex
	nextID      atomic.Uint64
	channels    map[string]map[*Subscriber]struct{}
	bufferLimit int
}

func NewHub() *Hub {
	return NewHubWithBuffer(DefaultSubscriberBuffer)
}

func NewHubWithBuffer(bufferLimit int) *Hub {
	if bufferLimit <= 0 {
		bufferLimit = DefaultSubscriberBuffer
	}
	return &Hub{
		channels:    make(map[string]map[*Subscriber]struct{}),
		bufferLimit: bufferLimit,
	}
}

func (h *Hub) NewSubscriber() *Subscriber {
	return &Subscriber{
		ID:       h.nextID.Add(1),
		Messages: make(chan Message, 2),
		channels: make(map[string]struct{}),
	}
}

func (h *Hub) Subscribe(sub *Subscriber, channel string) int {
	if sub == nil || channel == "" {
		return h.SubscriptionCount(sub)
	}

	h.mu.Lock()
	if h.channels[channel] == nil {
		h.channels[channel] = make(map[*Subscriber]struct{})
	}
	h.channels[channel][sub] = struct{}{}
	h.mu.Unlock()

	sub.mu.Lock()
	sub.channels[channel] = struct{}{}
	count := len(sub.channels)
	sub.mu.Unlock()

	return count
}

func (h *Hub) Unsubscribe(sub *Subscriber, channel string) int {
	if sub == nil || channel == "" {
		return h.SubscriptionCount(sub)
	}

	h.mu.Lock()
	if subscribers, ok := h.channels[channel]; ok {
		delete(subscribers, sub)
		if len(subscribers) == 0 {
			delete(h.channels, channel)
		}
	}
	h.mu.Unlock()

	sub.mu.Lock()
	delete(sub.channels, channel)
	count := len(sub.channels)
	sub.mu.Unlock()

	return count
}

func (h *Hub) UnsubscribeAll(sub *Subscriber) []string {
	if sub == nil {
		return nil
	}

	channels := sub.Channels()
	for _, channel := range channels {
		h.Unsubscribe(sub, channel)
	}
	return channels
}

func (h *Hub) Publish(channel string, payload []byte) int {
	h.mu.RLock()
	subscribers := make([]*Subscriber, 0, len(h.channels[channel]))
	for sub := range h.channels[channel] {
		subscribers = append(subscribers, sub)
	}
	h.mu.RUnlock()

	msg := Message{Channel: channel, Payload: append([]byte(nil), payload...)}
	for _, sub := range subscribers {
		sub.Messages <- msg
	}
	return len(subscribers)
}

func (h *Hub) SubscriptionCount(sub *Subscriber) int {
	if sub == nil {
		return 0
	}
	sub.mu.RLock()
	defer sub.mu.RUnlock()
	return len(sub.channels)
}

func (sub *Subscriber) Channels() []string {
	if sub == nil {
		return nil
	}
	sub.mu.RLock()
	defer sub.mu.RUnlock()

	channels := make([]string, 0, len(sub.channels))
	for channel := range sub.channels {
		channels = append(channels, channel)
	}
	return channels
}
