package store

import (
	"testing"
	"time"
)

func TestTTLStates(t *testing.T) {
	s := NewStore()

	if got := s.TTL("missing"); got != -2 {
		t.Fatalf("TTL missing = %d, want -2", got)
	}

	s.Set("plain", []byte("value"), 0)
	if got := s.TTL("plain"); got != -1 {
		t.Fatalf("TTL without expiry = %d, want -1", got)
	}

	s.Set("short", []byte("value"), 1)
	time.Sleep(2 * time.Millisecond)
	if got := s.TTL("short"); got != -2 {
		t.Fatalf("TTL expired = %d, want -2", got)
	}
	if _, ok := s.Get("short"); ok {
		t.Fatal("expired key should not be readable")
	}
}

func TestExpiredKeyCanChangeType(t *testing.T) {
	s := NewStore()
	s.Set("key", []byte("value"), 1)
	time.Sleep(2 * time.Millisecond)

	added, ok := s.SAdd("key", []byte("member"))
	if !ok || !added {
		t.Fatalf("SAdd after string expiry = added %v ok %v, want true true", added, ok)
	}
}

func TestListPushesReturnLengthAndWrongType(t *testing.T) {
	s := NewStore()

	if got, ok := s.LPush("list", []byte("a"), []byte("b")); !ok || got != 2 {
		t.Fatalf("LPush length = %d ok = %v, want 2 true", got, ok)
	}

	values, ok := s.LRange("list", 0, -1)
	if !ok || len(values) != 2 || string(values[0]) != "b" || string(values[1]) != "a" {
		t.Fatalf("LPush order = %q ok = %v, want [b a] true", values, ok)
	}

	s.Set("string", []byte("value"), 0)
	if _, ok := s.RPush("string", []byte("x")); ok {
		t.Fatal("RPush on string key should report wrong type")
	}
}
