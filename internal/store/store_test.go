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

func TestStringValuesAreCopied(t *testing.T) {
	s := NewStore()
	in := []byte("value")
	s.Set("key", in, 0)
	in[0] = 'X'

	got, ok := s.Get("key")
	if !ok || string(got) != "value" {
		t.Fatalf("stored value = %q ok = %v, want value true", got, ok)
	}

	got[0] = 'Y'
	gotAgain, ok := s.Get("key")
	if !ok || string(gotAgain) != "value" {
		t.Fatalf("stored value after caller mutation = %q ok = %v, want value true", gotAgain, ok)
	}
}

func TestLRangeNormalizesBounds(t *testing.T) {
	s := NewStore()
	if _, ok := s.RPush("list", []byte("a"), []byte("b"), []byte("c")); !ok {
		t.Fatal("RPush returned false")
	}

	values, ok := s.LRange("list", -2, 99)
	if !ok || len(values) != 2 || string(values[0]) != "b" || string(values[1]) != "c" {
		t.Fatalf("LRange(-2, 99) = %q ok = %v, want [b c] true", values, ok)
	}

	values, ok = s.LRange("list", 5, 10)
	if !ok || len(values) != 0 {
		t.Fatalf("LRange out of range = %q ok = %v, want empty true", values, ok)
	}
}

func TestSetAndHashOperations(t *testing.T) {
	s := NewStore()

	added, ok := s.SAdd("set", []byte("a"))
	if !ok || !added {
		t.Fatalf("first SAdd = added %v ok %v, want true true", added, ok)
	}
	added, ok = s.SAdd("set", []byte("a"))
	if !ok || added {
		t.Fatalf("duplicate SAdd = added %v ok %v, want false true", added, ok)
	}
	members, ok := s.SMembers("set")
	if !ok || len(members) != 1 || string(members[0]) != "a" {
		t.Fatalf("SMembers = %q ok = %v, want [a] true", members, ok)
	}

	created, ok := s.HSet("hash", "field", []byte("one"))
	if !ok || created != 1 {
		t.Fatalf("first HSet = %d ok = %v, want 1 true", created, ok)
	}
	created, ok = s.HSet("hash", "field", []byte("two"))
	if !ok || created != 0 {
		t.Fatalf("update HSet = %d ok = %v, want 0 true", created, ok)
	}
	val, ok := s.HGet("hash", "field")
	if !ok || string(val) != "two" {
		t.Fatalf("HGet = %q ok = %v, want two true", val, ok)
	}
}

func TestWrongTypeForSetAndHash(t *testing.T) {
	s := NewStore()
	s.Set("key", []byte("value"), 0)

	if _, ok := s.SAdd("key", []byte("member")); ok {
		t.Fatal("SAdd on string key should report wrong type")
	}
	if _, ok := s.HSet("key", "field", []byte("value")); ok {
		t.Fatal("HSet on string key should report wrong type")
	}
}
