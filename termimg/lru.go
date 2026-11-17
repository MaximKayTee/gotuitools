package termimg

import (
	"container/list"
	"sync"
)

// LRU keeps the n most recently used values. Safe for concurrent use.
type LRU[K comparable, V any] struct {
	mu  sync.Mutex
	cap int
	ll  *list.List
	m   map[K]*list.Element
	// Evicted runs on each value dropped for room, under the lock.
	Evicted func(K, V)
}

type lruItem[K comparable, V any] struct {
	k K
	v V
}

// NewLRU holds up to n values.
func NewLRU[K comparable, V any](n int) *LRU[K, V] {
	return &LRU[K, V]{cap: max(n, 1), ll: list.New(), m: map[K]*list.Element{}}
}

// Get returns k's value and marks it used.
func (c *LRU[K, V]) Get(k K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.m[k]
	if !ok {
		var zero V
		return zero, false
	}
	c.ll.MoveToFront(el)
	return el.Value.(*lruItem[K, V]).v, true
}

// Put stores k's value, dropping the least used beyond the capacity.
func (c *LRU[K, V]) Put(k K, v V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.m[k]; ok {
		el.Value.(*lruItem[K, V]).v = v
		c.ll.MoveToFront(el)
		return
	}
	c.m[k] = c.ll.PushFront(&lruItem[K, V]{k, v})
	for c.ll.Len() > c.cap {
		last := c.ll.Back()
		c.ll.Remove(last)
		it := last.Value.(*lruItem[K, V])
		delete(c.m, it.k)
		if c.Evicted != nil {
			c.Evicted(it.k, it.v)
		}
	}
}

// Remove drops k.
func (c *LRU[K, V]) Remove(k K) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.m[k]; ok {
		c.ll.Remove(el)
		delete(c.m, k)
	}
}

// Len is the number held.
func (c *LRU[K, V]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ll.Len()
}

// IDs hands out kitty image ids in [Lo, Hi), skipping ids in use.
// Kitty ids are 24-bit when sent as placeholder colours.
type IDs struct {
	mu     sync.Mutex
	Lo, Hi uint32
	next   uint32
	used   map[uint32]bool
}

// Take returns a free id; 0 when every id is in use.
func (s *IDs) Take() uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	lo, hi := max(s.Lo, 1), s.Hi
	if hi <= lo {
		hi = 0xFFFFFF
	}
	if s.used == nil {
		s.used = map[uint32]bool{}
	}
	if uint32(len(s.used)) >= hi-lo {
		return 0
	}
	for {
		if s.next < lo || s.next >= hi {
			s.next = lo
		}
		id := s.next
		s.next++
		if !s.used[id] {
			s.used[id] = true
			return id
		}
	}
}

// Free returns id.
func (s *IDs) Free(id uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.used, id)
}
