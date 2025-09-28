# ubch

Thread-safe, generic, unbounded channel for Go. `Send` never blocks; `Receive` blocks until a value or close.

Note: Prefer built-in `chan`. Use only for unbounded buffering; no backpressure, memory may grow.

## Install

```bash
go get github.com/mokeko/ubch
```

## Usage

```go
ch := ubch.NewChannel[int]()
go func() {
    ch.Send(1)
    ch.Send(2)
    ch.Close()
}()

for {
    v, ok := ch.Receive()
    if !ok {
        break
    }
    fmt.Println(v) // 1, 2
}
```
