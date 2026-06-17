package dispatcher

import (
	"bufio"
	"bytes"
	"testing"

	"redis-clone/internal/command"
	"redis-clone/internal/store"
)

func newTestContext() (*command.CommandContext, *bytes.Buffer) {
	var buf bytes.Buffer
	return &command.CommandContext{
		Writer:        command.NewRespWriter(bufio.NewWriter(&buf)),
		Store:         store.NewStore(),
		Authenticated: true,
	}, &buf
}

func TestDispatchUnknownCommandFails(t *testing.T) {
	ctx, _ := newTestContext()

	if Dispatch(ctx, []string{"NOPE"}) {
		t.Fatal("unknown command should return false")
	}
}

func TestDispatchExpire(t *testing.T) {
	ctx, buf := newTestContext()
	ctx.Store.Set("key", []byte("value"), 0)

	if !Dispatch(ctx, []string{"EXPIRE", "key", "10"}) {
		t.Fatal("EXPIRE should dispatch successfully")
	}
	if got := buf.String(); got != ":1\r\n" {
		t.Fatalf("EXPIRE response = %q, want %q", got, ":1\r\n")
	}
}

func TestDispatchInvalidLRangeFails(t *testing.T) {
	ctx, _ := newTestContext()

	if Dispatch(ctx, []string{"LRANGE", "key", "x", "1"}) {
		t.Fatal("LRANGE with non-integer start should return false")
	}
}
