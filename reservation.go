// Package intervalreservation provides a concurrency-safe, in-memory
// manager for non-overlapping half-open interval reservations.
package intervalreservation

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"sort"
	"sync"
)

var (
	// ErrEmptyID is returned when a reservation has an empty ID.
	ErrEmptyID = errors.New("intervalreservation: id must not be empty")
	// ErrInvalidRange is returned when Start >= End.
	ErrInvalidRange = errors.New("intervalreservation: start must be less than end")
	// ErrDuplicateID is returned when reserving an ID that already exists.
	ErrDuplicateID = errors.New("intervalreservation: id already exists")
	// ErrOverlap is returned (wrapped by *OverlapError) when an interval
	// overlaps an existing reservation.
	ErrOverlap = errors.New("intervalreservation: interval overlaps an existing reservation")
	// ErrNotFound is returned when no reservation exists for a given ID.
	ErrNotFound = errors.New("intervalreservation: reservation not found")
)

// OverlapError describes a rejected interval together with the existing
// reservation it conflicts with. It matches errors.Is(err, ErrOverlap).
type OverlapError struct {
	Conflict Reservation
}

func (e *OverlapError) Error() string {
	return fmt.Sprintf("%v: conflicts with %q [%d, %d)", ErrOverlap, e.Conflict.ID, e.Conflict.Start, e.Conflict.End)
}

func (e *OverlapError) Is(target error) bool { return target == ErrOverlap }

// Reservation is a half-open interval [Start, End) with a unique ID.
type Reservation struct {
	ID    string
	Start int64
	End   int64
}

// Manager stores non-overlapping reservations in a slice kept sorted by
// (Start, ID), so queries binary-search instead of re-sorting. A single
// RWMutex makes overlap-check-plus-write atomic across goroutines.
type Manager struct {
	mu     sync.RWMutex
	byID   map[string]Reservation
	sorted []Reservation
}

// NewManager returns an empty, ready-to-use Manager.
func NewManager() *Manager {
	return &Manager{byID: make(map[string]Reservation)}
}

func compare(a, b Reservation) int {
	if c := cmp.Compare(a.Start, b.Start); c != 0 {
		return c
	}
	return cmp.Compare(a.ID, b.ID)
}

func validate(r Reservation) error {
	if r.ID == "" {
		return ErrEmptyID
	}
	if r.Start >= r.End {
		return fmt.Errorf("%w: got [%d, %d)", ErrInvalidRange, r.Start, r.End)
	}
	return nil
}

// findConflict returns the first stored reservation overlapping
// [start, end), skipping ignoreID. Callers must hold the lock.
func (m *Manager) findConflict(start, end int64, ignoreID string) (Reservation, bool) {
	// Stored intervals are disjoint and sorted by Start, so their Ends are
	// also ascending: binary-search the first interval with End > start.
	i := sort.Search(len(m.sorted), func(i int) bool { return m.sorted[i].End > start })
	for ; i < len(m.sorted) && m.sorted[i].Start < end; i++ {
		if m.sorted[i].ID != ignoreID {
			return m.sorted[i], true
		}
	}
	return Reservation{}, false
}

// insertLocked adds r to both indexes. Callers must hold the write lock.
func (m *Manager) insertLocked(r Reservation) {
	pos, _ := slices.BinarySearchFunc(m.sorted, r, compare)
	m.sorted = slices.Insert(m.sorted, pos, r)
	m.byID[r.ID] = r
}

// removeLocked deletes r from both indexes. Callers must hold the write lock.
func (m *Manager) removeLocked(r Reservation) {
	if pos, found := slices.BinarySearchFunc(m.sorted, r, compare); found {
		m.sorted = slices.Delete(m.sorted, pos, pos+1)
	}
	delete(m.byID, r.ID)
}

// Reserve adds a new reservation. It fails with ErrEmptyID, ErrInvalidRange,
// ErrDuplicateID, or ErrOverlap (as *OverlapError) and changes nothing.
func (m *Manager) Reserve(r Reservation) error {
	if err := validate(r); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byID[r.ID]; ok {
		return fmt.Errorf("%w: %q", ErrDuplicateID, r.ID)
	}
	if c, ok := m.findConflict(r.Start, r.End, ""); ok {
		return &OverlapError{Conflict: c}
	}
	m.insertLocked(r)
	return nil
}

// Replace atomically moves the reservation identified by id to
// [start, end). The overlap check ignores the reservation itself. On any
// error the original reservation is left untouched.
func (m *Manager) Replace(id string, start, end int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	old, ok := m.byID[id]
	if !ok {
		return fmt.Errorf("%w: %q", ErrNotFound, id)
	}
	next := Reservation{ID: id, Start: start, End: end}
	if err := validate(next); err != nil {
		return err
	}
	if c, ok := m.findConflict(start, end, id); ok {
		return &OverlapError{Conflict: c}
	}
	m.removeLocked(old)
	m.insertLocked(next)
	return nil
}

// Release deletes the reservation with the given ID.
func (m *Manager) Release(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.byID[id]
	if !ok {
		return fmt.Errorf("%w: %q", ErrNotFound, id)
	}
	m.removeLocked(r)
	return nil
}

// Get returns the reservation with the given ID.
func (m *Manager) Get(id string) (Reservation, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.byID[id]
	return r, ok
}

// Find returns the reservation whose interval contains point
// (Start <= point < End).
func (m *Manager) Find(point int64) (Reservation, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	i := sort.Search(len(m.sorted), func(i int) bool { return m.sorted[i].End > point })
	if i < len(m.sorted) && m.sorted[i].Start <= point {
		return m.sorted[i], true
	}
	return Reservation{}, false
}

// Snapshot returns all reservations ordered by Start, then ID. The result
// is a copy and is fully isolated from later mutations of the Manager.
func (m *Manager) Snapshot() []Reservation {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return slices.Clone(m.sorted)
}

// Len returns the number of active reservations.
func (m *Manager) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.byID)
}
