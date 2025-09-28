package ubch

import (
    "context"
    "errors"
    "sync"
    "testing"
    "time"
)

func TestSendReceiveOrderAndLen(t *testing.T) {
    ch := NewChannel[int]()

    ch.Send(1)
    ch.Send(2)
    ch.Send(3)

    if got := ch.Len(); got != 3 {
        t.Fatalf("Len() = %d, want 3", got)
    }

    v, ok := ch.Receive()
    if !ok || v != 1 {
        t.Fatalf("Receive() = (%v,%v), want (1,true)", v, ok)
    }
    if got := ch.Len(); got != 2 {
        t.Fatalf("Len() = %d, want 2", got)
    }

    v, ok = ch.Receive()
    if !ok || v != 2 {
        t.Fatalf("Receive() = (%v,%v), want (2,true)", v, ok)
    }
    v, ok = ch.Receive()
    if !ok || v != 3 {
        t.Fatalf("Receive() = (%v,%v), want (3,true)", v, ok)
    }
}

func TestReceiveBlocksUntilSend(t *testing.T) {
    ch := NewChannel[int]()
    gotCh := make(chan int, 1)

    go func() {
        v, ok := ch.Receive()
        if ok {
            gotCh <- v
        }
    }()

    // Ensure it does not deliver immediately
    select {
    case <-gotCh:
        t.Fatal("Receive() returned before Send()")
    case <-time.After(10 * time.Millisecond):
        // expected: still blocking
    }

    ch.Send(42)

    select {
    case v := <-gotCh:
        if v != 42 {
            t.Fatalf("got %d, want 42", v)
        }
    case <-time.After(500 * time.Millisecond):
        t.Fatal("timed out waiting for Receive() after Send()")
    }
}

func TestCloseBehaviorAndPanicOnSendAfterClose(t *testing.T) {
    ch := NewChannel[int]()
    ch.Send(7)
    ch.Close()

    // Remaining value is still receivable
    v, ok := ch.Receive()
    if !ok || v != 7 {
        t.Fatalf("Receive() after Close = (%v,%v), want (7,true)", v, ok)
    }

    // Now empty and closed: should return zero and false
    v, ok = ch.Receive()
    if ok || v != 0 {
        t.Fatalf("Receive() on closed+empty = (%v,%v), want (0,false)", v, ok)
    }

    // Sending after close panics
    defer func() {
        if r := recover(); r == nil {
            t.Fatal("Send() did not panic after Close()")
        }
    }()
    ch.Send(1)
}

func TestCloseTwicePanics(t *testing.T) {
    ch := NewChannel[int]()
    ch.Close()
    defer func() {
        if r := recover(); r == nil {
            t.Fatal("second Close() did not panic")
        }
    }()
    ch.Close()
}

func TestReceiveContext_Basic(t *testing.T) {
    ch := NewChannel[int]()

    // Value available returns immediately
    ch.Send(99)
    v, err := ch.ReceiveContext(context.Background())
    if err != nil || v != 99 {
        t.Fatalf("ReceiveContext() = (%v,%v), want (99,<nil>)", v, err)
    }

    // Closed and empty -> ErrClosed
    ch.Close()
    _, err = ch.ReceiveContext(context.Background())
    if !errors.Is(err, ErrClosed) {
        t.Fatalf("ReceiveContext() error = %v, want ErrClosed", err)
    }
}

func TestReceiveContext_Cancel(t *testing.T) {
    ch := NewChannel[int]()
    ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
    defer cancel()

    start := time.Now()
    _, err := ch.ReceiveContext(ctx)
    if !errors.Is(err, context.DeadlineExceeded) {
        t.Fatalf("ReceiveContext() error = %v, want DeadlineExceeded", err)
    }
    if time.Since(start) < 15*time.Millisecond {
        // It should wait until near the deadline (not return immediately)
        t.Fatalf("ReceiveContext() returned too quickly: %v", time.Since(start))
    }
}

func TestReceiveChContext_ValueAndCancel(t *testing.T) {
    // Value case
    {
        ch := NewChannel[int]()
        ch.Send(5)
        ctx := context.Background()
        rch := ch.ReceiveChContext(ctx)
        select {
        case v, ok := <-rch:
            if !ok || v != 5 {
                t.Fatalf("ReceiveChContext value case got (%v,%v), want (5,true)", v, ok)
            }
        case <-time.After(500 * time.Millisecond):
            t.Fatal("timed out waiting for ReceiveChContext value")
        }
    }

    // Cancel before value arrives -> closed channel without value
    {
        ch := NewChannel[int]()
        ctx, cancel := context.WithCancel(context.Background())
        rch := ch.ReceiveChContext(ctx)
        cancel()
        select {
        case _, ok := <-rch:
            if ok {
                t.Fatal("ReceiveChContext delivered a value despite cancellation")
            }
        case <-time.After(500 * time.Millisecond):
            t.Fatal("timed out waiting for ReceiveChContext close after cancel")
        }
    }

    // Closed and empty -> closed channel without value
    {
        ch := NewChannel[int]()
        ch.Close()
        ctx := context.Background()
        rch := ch.ReceiveChContext(ctx)
        select {
        case _, ok := <-rch:
            if ok {
                t.Fatal("ReceiveChContext should be closed without value when channel closed+empty")
            }
        case <-time.After(500 * time.Millisecond):
            t.Fatal("timed out waiting for ReceiveChContext close on closed+empty")
        }
    }
}

func TestConcurrentSendReceive(t *testing.T) {
    ch := NewChannel[int]()
    const N = 1000

    var wg sync.WaitGroup
    wg.Add(2)

    go func() {
        defer wg.Done()
        for i := 0; i < N; i++ {
            ch.Send(i)
        }
        ch.Close()
    }()

    var sum int
    go func() {
        defer wg.Done()
        for {
            v, ok := ch.Receive()
            if !ok {
                return
            }
            sum += v
        }
    }()

    wg.Wait()

    want := (N - 1) * N / 2
    if sum != want {
        t.Fatalf("sum of received = %d, want %d", sum, want)
    }
}

