package ubch

import (
	"container/list"
	"context"
	"errors"
	"sync"
)

// Channel is a thread-safe, FIFO, unbounded queue with a channel-like API.
// Send never blocks; Receive blocks until a value is available or the channel is closed.
// This type provides no backpressure; the buffer can grow without bound.
// Not a drop-in replacement for built-in channels when backpressure is desired.
type Channel[T any] struct {
	l      list.List
	cond   *sync.Cond
	closed bool
}

// NewChannel returns a ready-to-use Channel[T].
// The zero value is not usable; always use NewChannel.
func NewChannel[T any]() *Channel[T] {
	return &Channel[T]{
		cond: sync.NewCond(&sync.Mutex{}),
	}
}

// Send enqueues v and wakes one waiting receiver.
// It never blocks. It panics if the channel has been closed.
func (c *Channel[T]) Send(v T) {
	c.cond.L.Lock()
	defer c.cond.L.Unlock()

	if c.closed {
		panic("send on closed channel")
	}

	c.l.PushBack(v)
	c.cond.Signal()
}

// ReceiveChContext returns a single-use channel that delivers at most one value.
// If a value is available, it sends the value; otherwise it closes the returned
// channel without a value when ctx is done or when the Channel is closed and empty.
// A goroutine is started per call; use sparingly.
func (c *Channel[T]) ReceiveChContext(ctx context.Context) <-chan T {
	ch := make(chan T, 1) // buffer size 1 to avoid goroutine leak

	go func() {
		defer close(ch)
		v, err := c.ReceiveContext(ctx)
		if err != nil {
			return // context canceled or channel closed
		}
		ch <- v
	}()

	return ch
}

// ErrClosed is returned by ReceiveContext when the Channel is closed and empty.
var ErrClosed = errors.New("channel closed")

// ReceiveContext receives a value honoring ctx cancellation.
//   - If a value is available, it returns (value, nil).
//   - If the Channel is closed and empty, it returns (zero, ErrClosed).
//   - If ctx is done first, it returns (zero, ctx.Err()).
// Internally, this may start a short-lived goroutine to observe ctx.Done().
func (c *Channel[T]) ReceiveContext(ctx context.Context) (T, error) {
	c.cond.L.Lock()
	defer c.cond.L.Unlock()

	done := make(chan struct{})
	defer close(done)

	go func() {
		// wait for context cancellation or done
		select {
		case <-ctx.Done():
			c.cond.L.Lock()
			c.cond.Broadcast() // wake up sleeping ReceiveContext
			c.cond.L.Unlock()
		case <-done:
			// ReceiveContext finished normally
		}
	}()

	for c.l.Len() == 0 && !c.closed {
		if ctx.Err() != nil {
			var zero T
			return zero, ctx.Err()
		}
		c.cond.Wait()
	}

	//  len > 0 || closed

	if c.l.Len() == 0 && c.closed {
		var zero T
		return zero, ErrClosed
	}

	// len > 0

	elem := c.l.Front()
	c.l.Remove(elem)
	return elem.Value.(T), nil
}

// Receive blocks until a value is available or the Channel is closed.
//   - If a value is available, it returns (value, true).
//   - If the Channel is closed and empty, it returns (zero, false).
func (c *Channel[T]) Receive() (T, bool) {
	c.cond.L.Lock()
	defer c.cond.L.Unlock()

	for c.l.Len() == 0 && !c.closed {
		c.cond.Wait()
	}

	//  len > 0 || closed

	if c.l.Len() == 0 && c.closed {
		var zero T
		return zero, false
	}

	// len > 0

	elem := c.l.Front()
	c.l.Remove(elem)
	return elem.Value.(T), true
}

// Len returns the current number of buffered elements.
// This is a snapshot and may change immediately after return.
func (c *Channel[T]) Len() int {
	c.cond.L.Lock()
	defer c.cond.L.Unlock()
	return c.l.Len()
}

// Close marks the channel as closed and wakes all waiters.
// Panics if called twice. After Close:
//   - Send panics.
//   - Receive continues to drain remaining values; when empty, it returns (zero, false).
//   - ReceiveContext returns ErrClosed when empty.
func (c *Channel[T]) Close() {
	c.cond.L.Lock()
	defer c.cond.L.Unlock()

	if c.closed {
		panic("close of closed channel")
	}

	c.closed = true
	c.cond.Broadcast()
}
