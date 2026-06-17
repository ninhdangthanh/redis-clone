package store

import (
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

	v := &Value{Type: StringType, Str: append([]byte(nil), val...)}
	if ttlMs > 0 {
		v.ExpiresAt = time.Now().UnixMilli() + ttlMs
	}
	s.data[key] = v
}

func (s *Store) SetAt(key string, val []byte, expiresAtMs int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	v := &Value{Type: StringType, Str: append([]byte(nil), val...)}
	if expiresAtMs > 0 {
		v.ExpiresAt = expiresAtMs
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
	return append([]byte(nil), v.Str...), true
}

func (s *Store) Del(keys []string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	deleted := 0
	now := time.Now().UnixMilli()
	for _, key := range keys {
		if s.deleteExpiredLocked(key, now) {
			continue
		}
		if _, ok := s.data[key]; ok {
			delete(s.data, key)
			deleted++
		}
	}
	return deleted
}

func (s *Store) LPush(key string, vals ...[]byte) (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	v, ok := s.data[key]
	if ok && s.valueExpiredLocked(key, v, time.Now().UnixMilli()) {
		ok = false
	}
	if !ok {
		v = &Value{Type: ListType}
		s.data[key] = v
	}
	if v.Type != ListType {
		return 0, false
	}

	newItems := make([][]byte, len(vals))
	for i, val := range vals {
		newItems[i] = append([]byte(nil), val...)
	}
	for i, j := 0, len(newItems)-1; i < j; i, j = i+1, j-1 {
		newItems[i], newItems[j] = newItems[j], newItems[i]
	}
	v.List = append(newItems, v.List...)
	return len(v.List), true
}

func (s *Store) RPush(key string, vals ...[]byte) (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	v, ok := s.data[key]
	if ok && s.valueExpiredLocked(key, v, time.Now().UnixMilli()) {
		ok = false
	}
	if !ok {
		v = &Value{Type: ListType}
		s.data[key] = v
	}
	if v.Type != ListType {
		return 0, false
	}

	for _, val := range vals {
		v.List = append(v.List, append([]byte(nil), val...))
	}
	return len(v.List), true
}

func (s *Store) LRange(key string, start, stop int) ([][]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	v, ok := s.data[key]
	if !ok || s.valueExpiredLocked(key, v, time.Now().UnixMilli()) {
		return nil, true
	}
	if v.Type != ListType {
		return nil, false
	}

	l := len(v.List)
	if l == 0 {
		return nil, true
	}
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
	if start >= l || start > stop {
		return nil, true
	}

	out := make([][]byte, 0, stop-start+1)
	for _, item := range v.List[start : stop+1] {
		out = append(out, append([]byte(nil), item...))
	}
	return out, true
}

func (s *Store) SAdd(key string, val []byte) (bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	v, ok := s.data[key]
	if ok && s.valueExpiredLocked(key, v, time.Now().UnixMilli()) {
		ok = false
	}
	if !ok {
		v = &Value{Type: SetType, Set: make(map[string]struct{})}
		s.data[key] = v
	}
	if v.Type != SetType {
		return false, false
	}

	_, exists := v.Set[string(val)]
	v.Set[string(val)] = struct{}{}
	return !exists, true
}

func (s *Store) SMembers(key string) ([][]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	v, ok := s.data[key]
	if !ok || s.valueExpiredLocked(key, v, time.Now().UnixMilli()) {
		return nil, true
	}
	if v.Type != SetType {
		return nil, false
	}

	members := make([][]byte, 0, len(v.Set))
	for k := range v.Set {
		members = append(members, []byte(k))
	}
	return members, true
}

func (s *Store) HSet(key, field string, val []byte) (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	v, ok := s.data[key]
	if ok && s.valueExpiredLocked(key, v, time.Now().UnixMilli()) {
		ok = false
	}
	if !ok {
		v = &Value{Type: HashType, Hash: make(map[string][]byte)}
		s.data[key] = v
	}
	if v.Type != HashType {
		return 0, false
	}

	_, exists := v.Hash[field]
	v.Hash[field] = append([]byte(nil), val...)

	if exists {
		return 0, true
	}
	return 1, true
}

func (s *Store) HGet(key, field string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	v, ok := s.data[key]
	if !ok || s.valueExpiredLocked(key, v, time.Now().UnixMilli()) || v.Type != HashType {
		return nil, false
	}

	val, exists := v.Hash[field]
	if !exists {
		return nil, false
	}
	return append([]byte(nil), val...), true
}

func (s *Store) valueExpiredLocked(key string, v *Value, now int64) bool {
	if v.ExpiresAt == 0 {
		return false
	}
	if now >= v.ExpiresAt {
		delete(s.data, key)
		return true
	}
	return false
}

func (s *Store) deleteExpiredLocked(key string, now int64) bool {
	v, ok := s.data[key]
	if !ok {
		return false
	}
	return s.valueExpiredLocked(key, v, now)
}

func (s *Store) Expire(key string, seconds int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.data[key]
	if !ok || s.valueExpiredLocked(key, v, time.Now().UnixMilli()) {
		return false
	}
	v.ExpiresAt = time.Now().UnixMilli() + seconds*1000
	return true
}

func (s *Store) ExpireAt(key string, expiresAtMs int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.data[key]
	if !ok || s.valueExpiredLocked(key, v, time.Now().UnixMilli()) {
		return false
	}
	v.ExpiresAt = expiresAtMs
	return true
}

func (s *Store) TTL(key string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.data[key]
	if !ok {
		return -2
	}
	if s.valueExpiredLocked(key, v, time.Now().UnixMilli()) {
		return -2
	}
	if v.ExpiresAt == 0 {
		return -1
	}
	ttl := v.ExpiresAt - time.Now().UnixMilli()
	if ttl <= 0 {
		delete(s.data, key)
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
					delete(s.data, key)
				}
			}
			s.mu.Unlock()
		}
	}()
}
