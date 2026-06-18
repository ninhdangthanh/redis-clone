package pubsub

import "testing"

func TestHubPublishDeliversToSubscribers(t *testing.T) {
	hub := NewHub()
	sub := hub.NewSubscriber()

	if count := hub.Subscribe(sub, "news"); count != 1 {
		t.Fatalf("Subscribe count = %d, want 1", count)
	}
	if receivers := hub.Publish("news", []byte("hello")); receivers != 1 {
		t.Fatalf("Publish receivers = %d, want 1", receivers)
	}

	msg := <-sub.Messages
	if msg.Channel != "news" || string(msg.Payload) != "hello" {
		t.Fatalf("message = (%q, %q), want (news, hello)", msg.Channel, msg.Payload)
	}
}

func TestHubUnsubscribeStopsDelivery(t *testing.T) {
	hub := NewHub()
	sub := hub.NewSubscriber()
	hub.Subscribe(sub, "news")

	if count := hub.Unsubscribe(sub, "news"); count != 0 {
		t.Fatalf("Unsubscribe count = %d, want 0", count)
	}
	if receivers := hub.Publish("news", []byte("hello")); receivers != 0 {
		t.Fatalf("Publish receivers = %d, want 0", receivers)
	}
}

func TestHubUnsubscribeAllReturnsSubscribedChannels(t *testing.T) {
	hub := NewHub()
	sub := hub.NewSubscriber()
	hub.Subscribe(sub, "a")
	hub.Subscribe(sub, "b")

	channels := hub.UnsubscribeAll(sub)
	if len(channels) != 2 {
		t.Fatalf("UnsubscribeAll returned %d channels, want 2", len(channels))
	}
	if count := hub.SubscriptionCount(sub); count != 0 {
		t.Fatalf("SubscriptionCount = %d, want 0", count)
	}
}
