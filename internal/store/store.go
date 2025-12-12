package store

import (
	"fmt"
	"sync"
	"time"
)

type ValueType int

const (
	StringType ValueType = iota
	ListType
	SetType
	HashType
)

type Value struct {
	Type      ValueType
	Str       []byte
	List      [][]byte
	Set       map[string]struct{}
	Hash      map[string][]byte
	ExpiresAt int64
}

type Store struct {
	mu   sync.RWMutex
	data map[string]*Value // key -> Value
}

func NewStore() *Store {
	return &Store{data: make(map[string]*Value)}
}

func (s *Store) Set(key string, val []byte, ttlMs int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	v := &Value{Type: StringType, Str: val}
	if ttlMs > 0 {
		v.ExpiresAt = time.Now().UnixMilli() + ttlMs
	}
	s.data[key] = v
}

func (s *Store) Get(key string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	v, ok := s.data[key]
	if !ok {
		return nil, false
	}

	if v.ExpiresAt > 0 && time.Now().UnixMilli() >= v.ExpiresAt {
		delete(s.data, key)
		return nil, false
	}

	if v.Type != StringType {
		return nil, false
	}
	return v.Str, true
}

func (s *Store) Del(keys []string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	deleted := 0
	for _, key := range keys {
		if _, ok := s.data[key]; ok {
			delete(s.data, key)
			deleted++
		}
	}
	return deleted
}

func (s *Store) LPush(key string, val []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	v, ok := s.data[key]
	if !ok {
		v = &Value{Type: ListType}
		s.data[key] = v
	}
	if v.Type != ListType {
		return
	}

	v.List = append([][]byte{val}, v.List...)
}

func (s *Store) RPush(key string, val []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	v, ok := s.data[key]
	if !ok {
		v = &Value{Type: ListType}
		s.data[key] = v
	}
	if v.Type != ListType {
		return
	}

	v.List = append(v.List, val)
}

func (s *Store) LRange(key string, start, stop int) [][]byte {
	s.mu.RLock()
	defer s.mu.RUnlock()

	v, ok := s.data[key]
	if !ok || v.Type != ListType {
		return nil
	}

	l := len(v.List)
	if start < 0 {
		start = l + start
	}
	if stop < 0 {
		stop = l + stop
	}
	if start < 0 {
		start = 0
	}
	if stop >= l {
		stop = l - 1
	}
	if start > stop {
		return nil
	}

	return v.List[start : stop+1]
}

func (s *Store) SAdd(key string, val []byte) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	v, ok := s.data[key]
	if !ok {
		v = &Value{Type: SetType, Set: make(map[string]struct{})}
		s.data[key] = v
	}
	if v.Type != SetType {
		return false
	}

	_, exists := v.Set[string(val)]
	v.Set[string(val)] = struct{}{}
	return !exists
}

func (s *Store) SMembers(key string) [][]byte {
	s.mu.RLock()
	defer s.mu.RUnlock()

	v, ok := s.data[key]
	if !ok || v.Type != SetType {
		return nil
	}

	members := make([][]byte, 0, len(v.Set))
	for k := range v.Set {
		members = append(members, []byte(k))
	}
	return members
}

func (s *Store) HSet(key, field string, val []byte) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	v, ok := s.data[key]
	if !ok {
		v = &Value{Type: HashType, Hash: make(map[string][]byte)}
		s.data[key] = v
	}
	if v.Type != HashType {
		return 0
	}

	_, exists := v.Hash[field]
	v.Hash[field] = val

	if exists {
		return 0
	}
	return 1
}

func (s *Store) HGet(key, field string) ([]byte, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	v, ok := s.data[key]
	if !ok || v.Type != HashType {
		return nil, false
	}

	val, exists := v.Hash[field]
	return val, exists
}

func (s *Store) isExpired(data map[string]*Value, key string) bool {
	v, ok := data[key]
	if !ok || v.ExpiresAt == 0 {
		return false
	}
	now := time.Now().UnixMilli()
	if now >= v.ExpiresAt {
		delete(data, key)
		return true
	}
	return false
}

func (s *Store) Expire(key string, seconds int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.data[key]
	if !ok {
		return false
	}
	v.ExpiresAt = time.Now().UnixMilli() + seconds*1000
	return true
}

func (s *Store) TTL(key string) int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[key]
	if !ok || v.ExpiresAt == 0 {
		return -1
	}
	ttl := v.ExpiresAt - time.Now().UnixMilli()
	if ttl < 0 {
		return -2
	}
	return ttl / 1000
}

func (s *Store) StartTTLChecker(interval time.Duration) {
	go func() {
		for {
			time.Sleep(interval)
			now := time.Now().UnixMilli()
			s.mu.Lock()
			for key, val := range s.data {
				if val.ExpiresAt > 0 && now >= val.ExpiresAt {
					fmt.Printf("Key '%s' expired at %d\n", key, now)
					delete(s.data, key)
				}
			}
			s.mu.Unlock()
		}
	}()
}
