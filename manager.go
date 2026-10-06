// Package intervalres provides a concurrency-safe, in-memory manager for
// non-overlapping, half-open interval reservations.
package intervalres

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

var (
	ErrEmptyID      = errors.New("intervalres: id must not be empty")
	ErrInvalidRange = errors.New("intervalres: start must be less than end")
	ErrDuplicateID  = errors.New("intervalres: id already exists")
	ErrNotFound     = errors.New("intervalres: id not found")
	ErrOverlap      = errors.New("intervalres: interval overlaps an existing reservation")
)

// Reservation is a half-open interval [Start, End) identified by a unique ID.
type Reservation struct {
	ID    string
	Start int64
	End   int64
}

// OverlapError reports that an interval conflicts with an existing
// reservation. It unwraps to ErrOverlap.
type OverlapError struct {
	Start    int64
	End      int64
	Conflict Reservation
}

func (e *OverlapError) Error() string {
	return fmt.Sprintf("intervalres: [%d, %d) overlaps existing reservation %q [%d, %d)",
		e.Start, e.End, e.Conflict.ID, e.Conflict.Start, e.Conflict.End)
}

func (e *OverlapError) Unwrap() error { return ErrOverlap }

// Manager stores non-overlapping interval reservations and is safe for
// concurrent use. Overlap checks and writes are performed atomically under
// a single lock, and reservations are kept in a slice sorted by Start so
// queries use binary search instead of re-sorting.
type Manager struct {
	mu     sync.RWMutex
	byID   map[string]*Reservation
	sorted []*Reservation
}

func NewManager() *Manager {
	return &Manager{byID: make(map[string]*Reservation)}
}

// Reserve adds a new reservation. It fails with ErrEmptyID, ErrInvalidRange,
// ErrDuplicateID, or ErrOverlap (via *OverlapError).
func (m *Manager) Reserve(id string, start, end int64) error {
	if id == "" {
		return ErrEmptyID
	}
	if start >= end {
		return ErrInvalidRange
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byID[id]; ok {
		return ErrDuplicateID
	}
	if c := m.conflict(start, end); c != nil {
		return &OverlapError{Start: start, End: end, Conflict: *c}
	}
	r := &Reservation{ID: id, Start: start, End: end}
	m.insertAt(m.search(start), r)
	m.byID[id] = r
	return nil
}

// Replace atomically changes the interval of an existing reservation,
// ignoring the reservation itself when checking for overlaps. On failure
// the original reservation is left unchanged.
func (m *Manager) Replace(id string, start, end int64) error {
	if start >= end {
		return ErrInvalidRange
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	old, ok := m.byID[id]
	if !ok {
		return ErrNotFound
	}
	oldIdx := m.search(old.Start)
	m.removeAt(oldIdx)
	if c := m.conflict(start, end); c != nil {
		m.insertAt(oldIdx, old)
		return &OverlapError{Start: start, End: end, Conflict: *c}
	}
	r := &Reservation{ID: id, Start: start, End: end}
	m.insertAt(m.search(start), r)
	m.byID[id] = r
	return nil
}

// Release removes the reservation with the given ID.
func (m *Manager) Release(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.byID[id]
	if !ok {
		return ErrNotFound
	}
	m.removeAt(m.search(r.Start))
	delete(m.byID, id)
	return nil
}

// Get returns the reservation with the given ID.
func (m *Manager) Get(id string) (Reservation, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.byID[id]
	if !ok {
		return Reservation{}, false
	}
	return *r, true
}

// Find returns the reservation whose interval contains point.
func (m *Manager) Find(point int64) (Reservation, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	idx := sort.Search(len(m.sorted), func(i int) bool { return m.sorted[i].Start > point })
	if idx == 0 {
		return Reservation{}, false
	}
	r := m.sorted[idx-1]
	if point >= r.End {
		return Reservation{}, false
	}
	return *r, true
}

// Snapshot returns a copy of all reservations ordered by Start (starts are
// unique, which also fixes the ID tie-break). The result is isolated from
// the manager's internal state.
func (m *Manager) Snapshot() []Reservation {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Reservation, len(m.sorted))
	for i, r := range m.sorted {
		out[i] = *r
	}
	return out
}

// Len returns the number of active reservations.
func (m *Manager) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.byID)
}

func (m *Manager) search(start int64) int {
	return sort.Search(len(m.sorted), func(i int) bool { return m.sorted[i].Start >= start })
}

func (m *Manager) conflict(start, end int64) *Reservation {
	idx := m.search(start)
	if idx > 0 && m.sorted[idx-1].End > start {
		return m.sorted[idx-1]
	}
	if idx < len(m.sorted) && m.sorted[idx].Start < end {
		return m.sorted[idx]
	}
	return nil
}

func (m *Manager) insertAt(idx int, r *Reservation) {
	m.sorted = append(m.sorted, nil)
	copy(m.sorted[idx+1:], m.sorted[idx:])
	m.sorted[idx] = r
}

func (m *Manager) removeAt(idx int) {
	m.sorted = append(m.sorted[:idx], m.sorted[idx+1:]...)
}
