package ubch

import (
	"container/list"
	"context"
	"errors"
	"sync"
)

// Channel is a thread-safe, unbounded channel that never blocks on send.
type Channel[T any] struct {
	l      list.List
	cond   *sync.Cond
	closed bool
}

// NewChannel creates a new Channel[T].
func NewChannel[T any]() *Channel[T] {
	return &Channel[T]{
		cond: sync.NewCond(&sync.Mutex{}),
	}
}

// Send sends a value to the channel.
// If the channel is closed, it panics.
// It never blocks.
func (c *Channel[T]) Send(v T) {
	c.cond.L.Lock()
	defer c.cond.L.Unlock()

	if c.closed {
		panic("send on closed channel")
	}

	c.l.PushBack(v)
	c.cond.Signal()
}

// ReceiveChContext returns a channel that will receive a value from the Channel[T].
// If the context is canceled before a value is available, the returned channel will be closed without sending a value.
// If the Channel[T] is closed and empty, the returned channel will be closed without sending a value.
// This method launches a goroutine per call, so use it cautiously.
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

// ErrClosed is returned when receiving from a closed channel with context.
var ErrClosed = errors.New("channel closed")

// ReceiveContext receives a value with a given context.
// If the context is canceled before a value is available, it returns ctx.Err().
// If the channel is closed and empty, it returns ErrClosed.
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

// Receive receives a value from the channel.
// If the channel is empty, it blocks until a value is available or the channel is closed.
// If the channel is closed and empty, it returns the zero value and false.
// Otherwise, it returns the value and true.
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

// Len returns the number of elements in the channel.
func (c *Channel[T]) Len() int {
	c.cond.L.Lock()
	defer c.cond.L.Unlock()
	return c.l.Len()
}

// Close closes the channel.
// It panics if the channel is already closed.
// After closing, no more values can be sent.
// Receivers can still receive remaining values until the channel is empty,
// after which they will receive zero values.
func (c *Channel[T]) Close() {
	c.cond.L.Lock()
	defer c.cond.L.Unlock()

	if c.closed {
		panic("close of closed channel")
	}

	c.closed = true
	c.cond.Broadcast()
}
