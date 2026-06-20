package command

import (
	"bufio"
	"bytes"
	"strings"
	"testing"

	"redis-clone/internal/pubsub"
	"redis-clone/internal/store"
)

func newCommandTestContext() (*CommandContext, *bytes.Buffer) {
	var buf bytes.Buffer
	return &CommandContext{
		Writer:        NewRespWriter(bufio.NewWriter(&buf)),
		Store:         store.NewStore(),
		Authenticated: true,
	}, &buf
}

func TestBasicCommands(t *testing.T) {
	tests := []struct {
		name string
		run  func(ctx *CommandContext) bool
		want string
	}{
		{
			name: "ping default",
			run:  func(ctx *CommandContext) bool { return HandlePing(ctx, []string{"PING"}) },
			want: "+PONG\r\n",
		},
		{
			name: "echo",
			run:  func(ctx *CommandContext) bool { return HandleEcho(ctx, []string{"ECHO", "hello"}) },
			want: "$5\r\nhello\r\n",
		},
		{
			name: "quit",
			run:  func(ctx *CommandContext) bool { return HandleQuit(ctx, []string{"QUIT"}) },
			want: "+OK\r\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, buf := newCommandTestContext()
			if !tt.run(ctx) {
				t.Fatal("command returned false")
			}
			if got := buf.String(); got != tt.want {
				t.Fatalf("response = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInfoCommand(t *testing.T) {
	ctx, buf := newCommandTestContext()
	if err := ctx.Store.Set("key", []byte("value"), 0); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}

	if !HandleInfo(ctx, []string{"INFO", "METRICS"}) {
		t.Fatal("INFO METRICS returned false")
	}
	response := buf.String()
	if !strings.Contains(response, "# Metrics\n") || !strings.Contains(response, "key_count:1\n") {
		t.Fatalf("INFO METRICS response = %q", response)
	}
	if strings.Contains(response, "# Config\n") || strings.Contains(response, "# State\n") {
		t.Fatalf("INFO METRICS should not include other sections: %q", response)
	}
}

func TestUnauthenticatedWriteCommandFails(t *testing.T) {
	ctx, buf := newCommandTestContext()
	ctx.Authenticated = false

	if HandleStringCommands(ctx, []string{"SET", "key", "value"}) {
		t.Fatal("unauthenticated SET should fail")
	}
	if got := buf.String(); got != "-NOAUTH Authentication required\r\n" {
		t.Fatalf("NOAUTH response = %q", got)
	}
}

func TestPublishReturnsSubscriberCount(t *testing.T) {
	ctx, buf := newCommandTestContext()
	ctx.PubSub = pubsub.NewHub()
	sub := ctx.PubSub.NewSubscriber()
	ctx.PubSub.Subscribe(sub, "news")

	if !HandlePublish(ctx, []string{"PUBLISH", "news", "hello"}) {
		t.Fatal("PUBLISH returned false")
	}
	if got := buf.String(); got != ":1\r\n" {
		t.Fatalf("PUBLISH response = %q, want :1", got)
	}

	msg := <-sub.Messages
	if msg.Channel != "news" || string(msg.Payload) != "hello" {
		t.Fatalf("published message = (%q, %q)", msg.Channel, msg.Payload)
	}
}
