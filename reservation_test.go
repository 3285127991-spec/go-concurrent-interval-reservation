package intervalreservation

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

func mustReserve(t *testing.T, m *Manager, id string, start, end int64) {
	t.Helper()
	if err := m.Reserve(Reservation{ID: id, Start: start, End: end}); err != nil {
		t.Fatalf("Reserve(%q [%d,%d)): %v", id, start, end, err)
	}
}

func TestReserveAndGet(t *testing.T) {
	m := NewManager()
	mustReserve(t, m, "a", 0, 10)
	mustReserve(t, m, "b", 20, 30)

	if got := m.Len(); got != 2 {
		t.Fatalf("Len() = %d, want 2", got)
	}
	r, ok := m.Get("a")
	if !ok || r.Start != 0 || r.End != 10 {
		t.Fatalf("Get(a) = %+v, %v", r, ok)
	}
	if _, ok := m.Get("missing"); ok {
		t.Fatal("Get(missing) should not be found")
	}
}

func TestReserveAdjacentAllowed(t *testing.T) {
	m := NewManager()
	mustReserve(t, m, "a", 0, 10)
	mustReserve(t, m, "b", 10, 20) // touches a's End
	mustReserve(t, m, "c", -10, 0) // touches a's Start

	if got := m.Len(); got != 3 {
		t.Fatalf("Len() = %d, want 3", got)
	}
}

func TestReserveOverlapRejected(t *testing.T) {
	m := NewManager()
	mustReserve(t, m, "a", 10, 20)

	cases := []Reservation{
		{ID: "left", Start: 5, End: 15},    // overlaps left side
		{ID: "right", Start: 15, End: 25},  // overlaps right side
		{ID: "inside", Start: 12, End: 18}, // contained in a
		{ID: "cover", Start: 0, End: 30},   // covers a
		{ID: "same", Start: 10, End: 20},   // identical interval
	}
	for _, r := range cases {
		err := m.Reserve(r)
		if !errors.Is(err, ErrOverlap) {
			t.Fatalf("Reserve(%q) error = %v, want ErrOverlap", r.ID, err)
		}
		var oe *OverlapError
		if !errors.As(err, &oe) || oe.Conflict.ID != "a" {
			t.Fatalf("Reserve(%q) conflict = %+v, want a", r.ID, err)
		}
	}
	if got := m.Len(); got != 1 {
		t.Fatalf("Len() = %d, want 1 after rejected reserves", got)
	}
}

func TestReserveDuplicateID(t *testing.T) {
	m := NewManager()
	mustReserve(t, m, "a", 0, 10)

	err := m.Reserve(Reservation{ID: "a", Start: 50, End: 60})
	if !errors.Is(err, ErrDuplicateID) {
		t.Fatalf("error = %v, want ErrDuplicateID", err)
	}
}

func TestReserveInvalid(t *testing.T) {
	m := NewManager()

	if err := m.Reserve(Reservation{ID: "", Start: 0, End: 1}); !errors.Is(err, ErrEmptyID) {
		t.Fatalf("empty ID error = %v, want ErrEmptyID", err)
	}
	if err := m.Reserve(Reservation{ID: "eq", Start: 5, End: 5}); !errors.Is(err, ErrInvalidRange) {
		t.Fatalf("Start==End error = %v, want ErrInvalidRange", err)
	}
	if err := m.Reserve(Reservation{ID: "neg", Start: 9, End: 3}); !errors.Is(err, ErrInvalidRange) {
		t.Fatalf("Start>End error = %v, want ErrInvalidRange", err)
	}
	if got := m.Len(); got != 0 {
		t.Fatalf("Len() = %d, want 0", got)
	}
}

func TestReplaceSuccess(t *testing.T) {
	m := NewManager()
	mustReserve(t, m, "a", 0, 10)
	mustReserve(t, m, "b", 20, 30)

	// Grow into the freed left gap and shrink elsewhere; overlaps with
	// the reservation's own old interval must be ignored.
	if err := m.Replace("a", 0, 15); err != nil {
		t.Fatalf("Replace(a): %v", err)
	}
	if err := m.Replace("b", 15, 25); err != nil {
		t.Fatalf("Replace(b): %v", err)
	}
	r, _ := m.Get("b")
	if r.Start != 15 || r.End != 25 {
		t.Fatalf("Get(b) = %+v, want [15,25)", r)
	}
	if got := m.Len(); got != 2 {
		t.Fatalf("Len() = %d, want 2", got)
	}
}

func TestReplaceFailureKeepsOriginal(t *testing.T) {
	m := NewManager()
	mustReserve(t, m, "a", 0, 10)
	mustReserve(t, m, "b", 20, 30)

	if err := m.Replace("b", 5, 25); !errors.Is(err, ErrOverlap) {
		t.Fatalf("Replace overlap error = %v, want ErrOverlap", err)
	}
	if err := m.Replace("b", 30, 20); !errors.Is(err, ErrInvalidRange) {
		t.Fatalf("Replace invalid range error = %v, want ErrInvalidRange", err)
	}
	if err := m.Replace("ghost", 40, 50); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Replace missing ID error = %v, want ErrNotFound", err)
	}

	r, ok := m.Get("b")
	if !ok || r.Start != 20 || r.End != 30 {
		t.Fatalf("Get(b) = %+v, %v; original must be unchanged", r, ok)
	}
	if got := m.Len(); got != 2 {
		t.Fatalf("Len() = %d, want 2", got)
	}
}

func TestRelease(t *testing.T) {
	m := NewManager()
	mustReserve(t, m, "a", 0, 10)
	mustReserve(t, m, "b", 10, 20)

	if err := m.Release("a"); err != nil {
		t.Fatalf("Release(a): %v", err)
	}
	if _, ok := m.Get("a"); ok {
		t.Fatal("Get(a) should be gone after Release")
	}
	if err := m.Release("a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second Release error = %v, want ErrNotFound", err)
	}
	// The freed interval can be reserved again.
	mustReserve(t, m, "c", 0, 10)
	if got := m.Len(); got != 2 {
		t.Fatalf("Len() = %d, want 2", got)
	}
}

func TestFind(t *testing.T) {
	m := NewManager()
	mustReserve(t, m, "a", 0, 10)
	mustReserve(t, m, "b", 10, 20)
	mustReserve(t, m, "c", 30, 40)

	if r, ok := m.Find(0); !ok || r.ID != "a" {
		t.Fatalf("Find(0) = %q, %v; want a", r.ID, ok)
	}
	if r, ok := m.Find(10); !ok || r.ID != "b" {
		t.Fatalf("Find(10) = %q, %v; want b (End is exclusive, Start inclusive)", r.ID, ok)
	}
	if _, ok := m.Find(20); ok {
		t.Fatal("Find(20) should miss: 20 is c's gap, b's End")
	}
	if _, ok := m.Find(25); ok {
		t.Fatal("Find(25) should miss the gap between b and c")
	}
	if r, ok := m.Find(39); !ok || r.ID != "c" {
		t.Fatalf("Find(39) = %q, %v; want c", r.ID, ok)
	}
	if _, ok := m.Find(40); ok {
		t.Fatal("Find(40) should miss: 40 is c's End")
	}
}

func TestSnapshotOrderAndIsolation(t *testing.T) {
	m := NewManager()
	// Insert out of order; Snapshot must come back sorted by Start.
	mustReserve(t, m, "z-last", 30, 40)
	mustReserve(t, m, "a-first", 0, 10)
	mustReserve(t, m, "m-mid", 10, 20)

	snap := m.Snapshot()
	want := []Reservation{
		{ID: "a-first", Start: 0, End: 10},
		{ID: "m-mid", Start: 10, End: 20},
		{ID: "z-last", Start: 30, End: 40},
	}
	if len(snap) != len(want) {
		t.Fatalf("Snapshot len = %d, want %d", len(snap), len(want))
	}
	for i, r := range want {
		if snap[i] != r {
			t.Fatalf("Snapshot()[%d] = %+v, want %+v", i, snap[i], r)
		}
	}

	// Mutating the snapshot must not affect the Manager.
	snap[0].ID = "corrupted"
	snap[1].Start = -999
	if r, _ := m.Get("a-first"); r.Start != 0 || r.End != 10 {
		t.Fatalf("manager state changed via snapshot: %+v", r)
	}
	if again := m.Snapshot(); again[0].ID != "a-first" || again[1].Start != 10 {
		t.Fatalf("second Snapshot differs: %+v", again)
	}
}

func TestConcurrentConflictingReserve(t *testing.T) {
	m := NewManager()
	const workers = 16

	var wg sync.WaitGroup
	var successes atomic.Int64
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Readers run alongside writers to exercise the RWMutex.
			m.Find(50)
			m.Snapshot()
			err := m.Reserve(Reservation{ID: fmt.Sprintf("r-%d", i), Start: 0, End: 100})
			switch {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, ErrOverlap):
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}(i)
	}
	wg.Wait()

	if got := successes.Load(); got != 1 {
		t.Fatalf("successful conflicting reserves = %d, want exactly 1", got)
	}
	if got := m.Len(); got != 1 {
		t.Fatalf("Len() = %d, want 1", got)
	}
}
