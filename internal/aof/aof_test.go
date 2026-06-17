package aof

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseMode(t *testing.T) {
	tests := []struct {
		input string
		want  FsyncMode
	}{
		{input: "always", want: FsyncAlways},
		{input: "ALWAYS", want: FsyncAlways},
		{input: "never", want: FsyncNever},
		{input: "unknown", want: FsyncEverySec},
		{input: "", want: FsyncEverySec},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := ParseMode(tt.input); got != tt.want {
				t.Fatalf("ParseMode(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

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

func TestReplayStopsOnCallbackError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "appendonly.aof")
	a, err := Open(path, FsyncNever)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer a.Close()

	if err := a.Append([]string{"SET", "key", "value"}); err != nil {
		t.Fatalf("Append returned error: %v", err)
	}

	wantErr := errors.New("stop")
	err = a.Replay(func(args []string) error {
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Replay error = %v, want %v", err, wantErr)
	}
}

func TestReplayRejectsInvalidFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "appendonly.aof")
	if err := os.WriteFile(path, []byte("+OK\r\n"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	a, err := Open(path, FsyncNever)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer a.Close()

	if err := a.Replay(func(args []string) error { return nil }); err == nil {
		t.Fatal("Replay should reject non-array AOF entries")
	}
}

func TestShouldPersistCommand(t *testing.T) {
	a := &AOF{}

	for _, cmd := range []string{"SET", "DEL", "LPUSH", "RPUSH", "SADD", "HSET", "EXPIRE"} {
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
