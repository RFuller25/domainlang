package ir

import "testing"

func TestQueueFIFO(t *testing.T) {
	var q Queue[int]
	if _, ok := q.Pop(); ok {
		t.Fatal("Pop on empty queue should report ok=false")
	}
	for i := 0; i < 10; i++ {
		q.Push(i)
	}
	if q.Len() != 10 {
		t.Fatalf("Len = %d, want 10", q.Len())
	}
	for i := 0; i < 10; i++ {
		v, ok := q.Pop()
		if !ok || v != i {
			t.Fatalf("Pop = %d,%v, want %d,true", v, ok, i)
		}
	}
	if q.Len() != 0 {
		t.Fatalf("Len after draining = %d, want 0", q.Len())
	}
}

// TestQueueWrap interleaves pushes and pops so the ring buffer must wrap.
func TestQueueWrap(t *testing.T) {
	var q Queue[int]
	next, expect := 0, 0
	for round := 0; round < 50; round++ {
		for i := 0; i < 3; i++ {
			q.Push(next)
			next++
		}
		for i := 0; i < 2; i++ {
			v, ok := q.Pop()
			if !ok || v != expect {
				t.Fatalf("round %d: Pop = %d,%v, want %d,true", round, v, ok, expect)
			}
			expect++
		}
	}
	for {
		v, ok := q.Pop()
		if !ok {
			break
		}
		if v != expect {
			t.Fatalf("drain: Pop = %d, want %d", v, expect)
		}
		expect++
	}
	if expect != next {
		t.Fatalf("drained %d values, want %d", expect, next)
	}
}

func TestStackLIFO(t *testing.T) {
	var s Stack[string]
	if _, ok := s.Pop(); ok {
		t.Fatal("Pop on empty stack should report ok=false")
	}
	s.Push("a")
	s.Push("b")
	s.Push("c")
	if s.Len() != 3 {
		t.Fatalf("Len = %d, want 3", s.Len())
	}
	for _, want := range []string{"c", "b", "a"} {
		v, ok := s.Pop()
		if !ok || v != want {
			t.Fatalf("Pop = %q,%v, want %q,true", v, ok, want)
		}
	}
	if _, ok := s.Pop(); ok {
		t.Fatal("stack should be empty")
	}
}

func TestPQOrdersByPriority(t *testing.T) {
	var q PQ[string]
	if _, _, ok := q.Pop(); ok {
		t.Fatal("Pop on empty PQ should report ok=false")
	}
	q.Push("mid", 5)
	q.Push("high", 1)
	q.Push("low", 9)
	q.Push("high2", 1) // same priority: insertion order breaks the tie
	if q.Len() != 4 {
		t.Fatalf("Len = %d, want 4", q.Len())
	}
	wants := []struct {
		v string
		p int64
	}{{"high", 1}, {"high2", 1}, {"mid", 5}, {"low", 9}}
	for _, w := range wants {
		v, p, ok := q.Pop()
		if !ok || v != w.v || p != w.p {
			t.Fatalf("Pop = %q,%d,%v, want %q,%d,true", v, p, ok, w.v, w.p)
		}
	}
}

func TestPQNegativePriorities(t *testing.T) {
	var q PQ[int]
	q.Push(1, 0)
	q.Push(2, -7)
	q.Push(3, 3)
	order := []int{2, 1, 3}
	for _, want := range order {
		v, _, ok := q.Pop()
		if !ok || v != want {
			t.Fatalf("Pop = %d,%v, want %d,true", v, ok, want)
		}
	}
}

func TestUnionFind(t *testing.T) {
	u := NewUnionFind(6)
	if u.Count() != 6 {
		t.Fatalf("Count = %d, want 6", u.Count())
	}
	if u.Connected(0, 1) {
		t.Fatal("fresh sets should not be connected")
	}
	if !u.Union(0, 1) {
		t.Fatal("Union(0,1) should merge")
	}
	if u.Union(1, 0) {
		t.Fatal("Union(1,0) should be a no-op the second time")
	}
	u.Union(2, 3)
	u.Union(1, 2) // {0,1,2,3}, {4}, {5}
	if u.Count() != 3 {
		t.Fatalf("Count = %d, want 3", u.Count())
	}
	if !u.Connected(0, 3) {
		t.Fatal("0 and 3 should be connected transitively")
	}
	if u.Connected(0, 4) || u.Connected(4, 5) {
		t.Fatal("4 and 5 should remain singletons")
	}
	if u.Find(0) != u.Find(3) {
		t.Fatal("Find should agree for merged elements")
	}
}

func TestMemoComputesOnce(t *testing.T) {
	var m Memo[string, int]
	calls := 0
	f := func() int { calls++; return 42 }
	if got := m.Get("k", f); got != 42 {
		t.Fatalf("Get = %d, want 42", got)
	}
	if got := m.Get("k", f); got != 42 {
		t.Fatalf("Get (cached) = %d, want 42", got)
	}
	if calls != 1 {
		t.Fatalf("compute ran %d times, want 1", calls)
	}
	if !m.Has("k") || m.Has("other") {
		t.Fatal("Has should reflect memoized keys only")
	}
	m.Get("other", func() int { return 7 })
	if m.Len() != 2 {
		t.Fatalf("Len = %d, want 2", m.Len())
	}
}
