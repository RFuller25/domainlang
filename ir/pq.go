package ir

import "container/heap"

// PQ is a min-priority queue: Pop returns the element with the smallest
// priority. Elements with equal priority come out in insertion order (the
// tie-break keeps Dijkstra deterministic). The zero value is ready to use.
type PQ[T any] struct {
	h pqHeap[T]
}

type pqItem[T any] struct {
	value    T
	priority int64
	seq      int64 // insertion order, breaks priority ties
}

// Push inserts v with the given priority.
func (q *PQ[T]) Push(v T, priority int64) {
	item := pqItem[T]{value: v, priority: priority, seq: q.h.nextSeq}
	q.h.nextSeq++
	heap.Push(&q.h, item)
}

// Pop removes and returns the element with the smallest priority; ok is false
// on an empty queue.
func (q *PQ[T]) Pop() (T, int64, bool) {
	if q.h.Len() == 0 {
		var zero T
		return zero, 0, false
	}
	item := heap.Pop(&q.h).(pqItem[T])
	return item.value, item.priority, true
}

// Len reports the number of queued elements.
func (q *PQ[T]) Len() int { return q.h.Len() }

// pqHeap implements container/heap.Interface over pqItems.
type pqHeap[T any] struct {
	items   []pqItem[T]
	nextSeq int64
}

func (h *pqHeap[T]) Len() int { return len(h.items) }
func (h *pqHeap[T]) Less(i, j int) bool {
	a, b := h.items[i], h.items[j]
	if a.priority != b.priority {
		return a.priority < b.priority
	}
	return a.seq < b.seq
}
func (h *pqHeap[T]) Swap(i, j int) { h.items[i], h.items[j] = h.items[j], h.items[i] }
func (h *pqHeap[T]) Push(x any)    { h.items = append(h.items, x.(pqItem[T])) }
func (h *pqHeap[T]) Pop() any {
	old := h.items
	n := len(old)
	item := old[n-1]
	h.items = old[:n-1]
	return item
}
