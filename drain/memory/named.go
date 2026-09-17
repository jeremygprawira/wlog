package memory

import (
	"sort"
	"sync"
)

// registry holds the named stores. The spec allows this one piece of package-level
// state, because a debug endpoint in one package and a handler in another must reach the
// same buffer without a pointer passed between them.
var registry = struct {
	sync.Mutex
	stores map[string]*Memory
}{stores: map[string]*Memory{}}

// Named returns the store registered under name, creating it with size on first use.
// The same name always returns the same store, so two packages share one buffer. The
// first registration wins, including its size.
func Named(name string, size int) *Memory {
	registry.Lock()
	defer registry.Unlock()
	if store, ok := registry.stores[name]; ok {
		return store
	}
	store := New(size)
	registry.stores[name] = store
	return store
}

// Remove unregisters a store, so a later Named call with the same name builds a fresh one.
// A test uses it to leave the registry as it found it.
func Remove(name string) {
	registry.Lock()
	defer registry.Unlock()
	delete(registry.stores, name)
}

// Stores returns every registered store name, sorted.
func Stores() []string {
	registry.Lock()
	defer registry.Unlock()
	names := make([]string, 0, len(registry.stores))
	for name := range registry.stores {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Clear empties the store and keeps its size, so it can keep receiving events. A
// subscriber channel is left alone, since it owns its own buffer.
func (m *Memory) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.buf = nil
	m.next = 0
}
