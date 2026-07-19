package ir

// Generic FIFO queue and LIFO stack, the traversal workhorses behind the
// graph-search primitives (BFS uses Queue; Flood Fill uses Stack). They live
// in ir next to the other runtime collections so any primitive can use them.

// Queue is a FIFO queue: Push appends to the back, Pop removes from the front.
// The zero value is ready to use. It is implemented as a growable ring buffer
// so a long-lived queue does not leak its consumed prefix.
type Queue[T any] struct {
	buf   []T
	head  int
	count int
}

// Push appends v to the back of the queue.
func (q *Queue[T]) Push(v T) {
	if q.count == len(q.buf) {
		q.grow()
	}
	q.buf[(q.head+q.count)%len(q.buf)] = v
	q.count++
}

// Pop removes and returns the front element; ok is false on an empty queue.
func (q *Queue[T]) Pop() (T, bool) {
	var zero T
	if q.count == 0 {
		return zero, false
	}
	v := q.buf[q.head]
	q.buf[q.head] = zero // release the slot for GC
	q.head = (q.head + 1) % len(q.buf)
	q.count--
	return v, true
}

// Len reports the number of queued elements.
func (q *Queue[T]) Len() int { return q.count }

func (q *Queue[T]) grow() {
	next := make([]T, max(4, len(q.buf)*2))
	for i := 0; i < q.count; i++ {
		next[i] = q.buf[(q.head+i)%len(q.buf)]
	}
	q.buf = next
	q.head = 0
}

// Stack is a LIFO stack: Push and Pop both work at the top. The zero value is
// ready to use.
type Stack[T any] struct {
	elems []T
}

// Push places v on top of the stack.
func (s *Stack[T]) Push(v T) { s.elems = append(s.elems, v) }

// Pop removes and returns the top element; ok is false on an empty stack.
func (s *Stack[T]) Pop() (T, bool) {
	var zero T
	if len(s.elems) == 0 {
		return zero, false
	}
	v := s.elems[len(s.elems)-1]
	s.elems[len(s.elems)-1] = zero
	s.elems = s.elems[:len(s.elems)-1]
	return v, true
}

// Len reports the number of stacked elements.
func (s *Stack[T]) Len() int { return len(s.elems) }
