package intervalres

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

func mustReserve(t *testing.T, m *Manager, id string, start, end int64) {
	t.Helper()
	if err := m.Reserve(id, start, end); err != nil {
		t.Fatalf("Reserve(%q, %d, %d) failed: %v", id, start, end, err)
	}
}

func TestReserveAndGet(t *testing.T) {
	m := NewManager()
	mustReserve(t, m, "a", 0, 10)
	mustReserve(t, m, "b", 20, 30)
	r, ok := m.Get("a")
	if !ok || r.ID != "a" || r.Start != 0 || r.End != 10 {
		t.Fatalf("Get(a) = %+v, %v", r, ok)
	}
	if _, ok := m.Get("missing"); ok {
		t.Fatal("Get(missing) should report not found")
	}
	if got := m.Len(); got != 2 {
		t.Fatalf("Len = %d, want 2", got)
	}
}

func TestReserveAdjacentAllowed(t *testing.T) {
	m := NewManager()
	mustReserve(t, m, "a", 0, 5)
	mustReserve(t, m, "b", 5, 10)
	mustReserve(t, m, "c", -10, 0)
	if got := m.Len(); got != 3 {
		t.Fatalf("Len = %d, want 3", got)
	}
}

func TestReserveOverlapRejected(t *testing.T) {
	m := NewManager()
	mustReserve(t, m, "a", 0, 5)
	mustReserve(t, m, "b", 10, 20)
	mustReserve(t, m, "c", 25, 30)
	cases := []struct {
		name       string
		start, end int64
		conflictID string
	}{
		{"same start", 0, 3, "a"},
		{"inside", 1, 4, "a"},
		{"covers left", 2, 6, "a"},
		{"touches right", 12, 15, "b"},
		{"exact match", 10, 20, "b"},
		{"spans gap", 4, 26, "a"},
		{"covers all", -100, 100, "a"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := m.Reserve("x", tc.start, tc.end)
			if !errors.Is(err, ErrOverlap) {
				t.Fatalf("Reserve(x, %d, %d) error = %v, want ErrOverlap", tc.start, tc.end, err)
			}
			var oe *OverlapError
			if !errors.As(err, &oe) {
				t.Fatalf("error %v is not an *OverlapError", err)
			}
			if oe.Conflict.ID != tc.conflictID {
				t.Fatalf("conflict = %q, want %q", oe.Conflict.ID, tc.conflictID)
			}
		})
	}
	if got := m.Len(); got != 3 {
		t.Fatalf("Len = %d after rejected reserves, want 3", got)
	}
}

func TestReserveDuplicateID(t *testing.T) {
	m := NewManager()
	mustReserve(t, m, "a", 0, 5)
	if err := m.Reserve("a", 10, 20); !errors.Is(err, ErrDuplicateID) {
		t.Fatalf("duplicate Reserve error = %v, want ErrDuplicateID", err)
	}
	r, _ := m.Get("a")
	if r.Start != 0 || r.End != 5 {
		t.Fatalf("reservation changed after duplicate reserve: %+v", r)
	}
	if got := m.Len(); got != 1 {
		t.Fatalf("Len = %d, want 1", got)
	}
}

func TestReserveInvalidInput(t *testing.T) {
	m := NewManager()
	if err := m.Reserve("", 0, 10); !errors.Is(err, ErrEmptyID) {
		t.Fatalf("empty id error = %v, want ErrEmptyID", err)
	}
	if err := m.Reserve("x", 10, 10); !errors.Is(err, ErrInvalidRange) {
		t.Fatalf("empty range error = %v, want ErrInvalidRange", err)
	}
	if err := m.Reserve("x", 20, 10); !errors.Is(err, ErrInvalidRange) {
		t.Fatalf("reversed range error = %v, want ErrInvalidRange", err)
	}
	if got := m.Len(); got != 0 {
		t.Fatalf("Len = %d, want 0", got)
	}
}

func TestReplaceSuccess(t *testing.T) {
	m := NewManager()
	mustReserve(t, m, "a", 0, 10)
	mustReserve(t, m, "b", 10, 20)
	mustReserve(t, m, "c", 30, 40)
	if err := m.Replace("c", 20, 30); err != nil {
		t.Fatalf("Replace(c, 20, 30) failed: %v", err)
	}
	r, _ := m.Get("c")
	if r.Start != 20 || r.End != 30 {
		t.Fatalf("Get(c) = %+v, want [20, 30)", r)
	}
	if _, ok := m.Find(35); ok {
		t.Fatal("Find(35) should miss after replace")
	}
	if r, ok := m.Find(25); !ok || r.ID != "c" {
		t.Fatalf("Find(25) = %+v, %v, want c", r, ok)
	}
	if err := m.Replace("a", 2, 10); err != nil {
		t.Fatalf("Replace(a, 2, 10) should ignore self and stay adjacent to b: %v", err)
	}
	if got := m.Len(); got != 3 {
		t.Fatalf("Len = %d, want 3", got)
	}
}

func TestReplaceFailureKeepsOriginal(t *testing.T) {
	m := NewManager()
	mustReserve(t, m, "a", 0, 10)
	mustReserve(t, m, "b", 10, 20)
	if err := m.Replace("a", 5, 15); !errors.Is(err, ErrOverlap) {
		t.Fatalf("Replace(a, 5, 15) error = %v, want ErrOverlap", err)
	}
	if err := m.Replace("a", 15, 10); !errors.Is(err, ErrInvalidRange) {
		t.Fatalf("Replace(a, 15, 10) error = %v, want ErrInvalidRange", err)
	}
	if err := m.Replace("ghost", 30, 40); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Replace(ghost) error = %v, want ErrNotFound", err)
	}
	r, ok := m.Get("a")
	if !ok || r.Start != 0 || r.End != 10 {
		t.Fatalf("Get(a) = %+v, %v, want original [0, 10)", r, ok)
	}
	if got := m.Len(); got != 2 {
		t.Fatalf("Len = %d, want 2", got)
	}
}

func TestRelease(t *testing.T) {
	m := NewManager()
	mustReserve(t, m, "a", 0, 10)
	mustReserve(t, m, "b", 10, 20)
	if err := m.Release("a"); err != nil {
		t.Fatalf("Release(a) failed: %v", err)
	}
	if _, ok := m.Get("a"); ok {
		t.Fatal("Get(a) should miss after release")
	}
	if _, ok := m.Find(5); ok {
		t.Fatal("Find(5) should miss after release")
	}
	if err := m.Release("a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second Release(a) error = %v, want ErrNotFound", err)
	}
	if got := m.Len(); got != 1 {
		t.Fatalf("Len = %d, want 1", got)
	}
	mustReserve(t, m, "c", 0, 10)
}

func TestFind(t *testing.T) {
	m := NewManager()
	if _, ok := m.Find(1); ok {
		t.Fatal("Find on empty manager should miss")
	}
	mustReserve(t, m, "a", 0, 10)
	mustReserve(t, m, "b", 10, 20)
	mustReserve(t, m, "c", 30, 40)
	cases := []struct {
		point   int64
		wantID  string
		wantHit bool
	}{
		{-1, "", false},
		{0, "a", true},
		{9, "a", true},
		{10, "b", true},
		{20, "", false},
		{25, "", false},
		{39, "c", true},
		{40, "", false},
	}
	for _, tc := range cases {
		r, ok := m.Find(tc.point)
		if ok != tc.wantHit || (ok && r.ID != tc.wantID) {
			t.Fatalf("Find(%d) = %+v, %v, want %q, %v", tc.point, r, ok, tc.wantID, tc.wantHit)
		}
	}
}

func TestSnapshotOrderAndIsolation(t *testing.T) {
	m := NewManager()
	mustReserve(t, m, "c", 20, 30)
	mustReserve(t, m, "a", 0, 10)
	mustReserve(t, m, "b", 10, 20)
	snap := m.Snapshot()
	if len(snap) != 3 {
		t.Fatalf("Snapshot len = %d, want 3", len(snap))
	}
	want := []Reservation{
		{ID: "a", Start: 0, End: 10},
		{ID: "b", Start: 10, End: 20},
		{ID: "c", Start: 20, End: 30},
	}
	for i, w := range want {
		if snap[i] != w {
			t.Fatalf("Snapshot()[%d] = %+v, want %+v", i, snap[i], w)
		}
	}
	snap[0].ID = "hacked"
	snap[1] = Reservation{}
	again := m.Snapshot()
	for i, w := range want {
		if again[i] != w {
			t.Fatalf("after mutating snapshot, Snapshot()[%d] = %+v, want %+v", i, again[i], w)
		}
	}
	if r, _ := m.Get("a"); r.Start != 0 || r.End != 10 {
		t.Fatalf("internal state changed via snapshot: %+v", r)
	}
}

func TestConcurrentConflictingReserve(t *testing.T) {
	m := NewManager()
	const workers = 32
	var succeeded, overlaps atomic.Int32
	stop := make(chan struct{})
	var readers sync.WaitGroup
	for i := 0; i < 4; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
					m.Find(150)
					m.Snapshot()
					m.Len()
				}
			}
		}()
	}
	var writers sync.WaitGroup
	for i := 0; i < workers; i++ {
		writers.Add(1)
		go func(i int) {
			defer writers.Done()
			err := m.Reserve(fmt.Sprintf("r-%d", i), 100, 200)
			switch {
			case err == nil:
				succeeded.Add(1)
			case errors.Is(err, ErrOverlap):
				overlaps.Add(1)
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}(i)
	}
	writers.Wait()
	close(stop)
	readers.Wait()
	if got := succeeded.Load(); got != 1 {
		t.Fatalf("successful reserves = %d, want 1", got)
	}
	if got := overlaps.Load(); got != workers-1 {
		t.Fatalf("overlap rejections = %d, want %d", got, workers-1)
	}
	if got := m.Len(); got != 1 {
		t.Fatalf("Len = %d, want 1", got)
	}
	if _, ok := m.Find(150); !ok {
		t.Fatal("Find(150) should locate the winning reservation")
	}
}
