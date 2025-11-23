package internal

import "sync"

type ValueType int

const (
	StringType ValueType = iota
	ListType
	SetType
	HashType
)

type Value struct {
	Type ValueType
	Str  []byte
	List [][]byte
	Set  map[string]struct{}
	Hash map[string][]byte
}

type Store struct {
	mu   sync.RWMutex
	data map[string]map[string]*Value
}

func NewStore() *Store {
	return &Store{data: make(map[string]map[string]*Value)}
}

func (s *Store) getUserData(user string) map[string]*Value {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[user]; !ok {
		s.data[user] = make(map[string]*Value)
	}
	return s.data[user]
}

func (s *Store) Set(user, key string, val []byte) {
	ud := s.getUserData(user)
	s.mu.Lock()
	defer s.mu.Unlock()
	ud[key] = &Value{Type: StringType, Str: val}
}

func (s *Store) Get(user, key string) ([]byte, bool) {
	ud := s.getUserData(user)
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := ud[key]
	if !ok || v.Type != StringType {
		return nil, false
	}
	return v.Str, true
}

func (s *Store) LPush(user, key string, val []byte) {
	ud := s.getUserData(user)
	s.mu.Lock()
	defer s.mu.Unlock()

	v, ok := ud[key]
	if !ok {
		v = &Value{Type: ListType}
		ud[key] = v
	}
	if v.Type != ListType {
		return
	}

	v.List = append([][]byte{val}, v.List...)
}

func (s *Store) RPush(user, key string, val []byte) {
	ud := s.getUserData(user)
	s.mu.Lock()
	defer s.mu.Unlock()

	v, ok := ud[key]
	if !ok {
		v = &Value{Type: ListType}
		ud[key] = v
	}
	if v.Type != ListType {
		return
	}

	v.List = append(v.List, val)
}

func (s *Store) LRange(user, key string, start, stop int) [][]byte {
	ud := s.getUserData(user)
	s.mu.RLock()
	defer s.mu.RUnlock()

	v, ok := ud[key]
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

func (s *Store) SAdd(user, key string, val []byte) bool {
	ud := s.getUserData(user)
	s.mu.Lock()
	defer s.mu.Unlock()

	v, ok := ud[key]
	if !ok {
		v = &Value{Type: SetType, Set: make(map[string]struct{})}
		ud[key] = v
	}
	if v.Type != SetType {
		return false
	}

	_, exists := v.Set[string(val)]
	v.Set[string(val)] = struct{}{}
	return !exists
}

func (s *Store) SMembers(user, key string) [][]byte {
	ud := s.getUserData(user)
	s.mu.RLock()
	defer s.mu.RUnlock()

	v, ok := ud[key]
	if !ok || v.Type != SetType {
		return nil
	}

	members := make([][]byte, 0, len(v.Set))
	for k := range v.Set {
		members = append(members, []byte(k))
	}
	return members
}

func (s *Store) HSet(user, key, field string, val []byte) int {
	ud := s.getUserData(user)
	s.mu.Lock()
	defer s.mu.Unlock()

	v, ok := ud[key]
	if !ok {
		v = &Value{Type: HashType, Hash: make(map[string][]byte)}
		ud[key] = v
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

func (s *Store) HGet(user, key, field string) ([]byte, bool) {
	ud := s.getUserData(user)
	s.mu.RLock()
	defer s.mu.RUnlock()

	v, ok := ud[key]
	if !ok || v.Type != HashType {
		return nil, false
	}

	val, exists := v.Hash[field]
	return val, exists
}
