package aof

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestAppendAndReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "appendonly.aof")
	a, err := Open(path, FsyncNever)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer a.Close()

	commands := [][]string{
		{"SET", "key", "value"},
		{"LPUSH", "list", "a", "b"},
	}
	for _, cmd := range commands {
		if err := a.Append(cmd); err != nil {
			t.Fatalf("Append(%v) returned error: %v", cmd, err)
		}
	}

	var replayed [][]string
	if err := a.Replay(func(args []string) error {
		replayed = append(replayed, args)
		return nil
	}); err != nil {
		t.Fatalf("Replay returned error: %v", err)
	}

	if !reflect.DeepEqual(replayed, commands) {
		t.Fatalf("replayed commands = %#v, want %#v", replayed, commands)
	}
}

func TestArgsForAppendConvertsRelativeExpiries(t *testing.T) {
	now := time.UnixMilli(1_000_000)

	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "set ex",
			args: []string{"SET", "key", "value", "EX", "10"},
			want: []string{"SET", "key", "value", "PXAT", "1010000"},
		},
		{
			name: "set px",
			args: []string{"set", "key", "value", "px", "25"},
			want: []string{"SET", "key", "value", "PXAT", "1000025"},
		},
		{
			name: "expire",
			args: []string{"EXPIRE", "key", "10"},
			want: []string{"PEXPIREAT", "key", "1010000"},
		},
		{
			name: "plain set",
			args: []string{"SET", "key", "value"},
			want: []string{"SET", "key", "value"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := append([]string(nil), tt.args...)
			got := ArgsForAppend(tt.args, now)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ArgsForAppend(%v) = %v, want %v", tt.args, got, tt.want)
			}
			if !reflect.DeepEqual(tt.args, original) {
				t.Fatalf("ArgsForAppend mutated input to %v, want %v", tt.args, original)
			}
		})
	}
}

func TestShouldPersistCommand(t *testing.T) {
	a := &AOF{}

	for _, cmd := range []string{"SET", "DEL", "LPUSH", "RPUSH", "SADD", "HSET", "EXPIRE", "PEXPIREAT"} {
		if !a.ShouldPersistCommand(cmd) {
			t.Fatalf("ShouldPersistCommand(%q) = false, want true", cmd)
		}
	}
	for _, cmd := range []string{"GET", "TTL", "PING", "UNKNOWN"} {
		if a.ShouldPersistCommand(cmd) {
			t.Fatalf("ShouldPersistCommand(%q) = true, want false", cmd)
		}
	}
}
