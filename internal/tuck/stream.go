package tuck

import (
	"context"
	"io"
	"sync"
	"time"
)

const fileTransferIdleTimeout = 120 * time.Second

// Only inactivity ends file transfers; native executions and MCP/SSE do not use
// this wrapper. Cancellation belongs to this HTTP operation, not its transport.
type idleReadCloser struct {
	io.ReadCloser
	mu            sync.Mutex
	timer         *time.Timer
	idle          time.Duration
	cancel        context.CancelFunc
	closed        bool
	cancelOnClose bool
}

func watchIdle(body io.ReadCloser, idle time.Duration, cancel context.CancelFunc) *idleReadCloser {
	b := &idleReadCloser{ReadCloser: body, idle: idle, cancel: cancel, cancelOnClose: true}
	b.timer = time.AfterFunc(idle, cancel)
	return b
}
func watchRequestIdle(body io.ReadCloser, idle time.Duration, cancel context.CancelFunc) *idleReadCloser {
	b := watchIdle(body, idle, cancel)
	b.cancelOnClose = false
	return b
}
func (b *idleReadCloser) Read(p []byte) (int, error) {
	n, e := b.ReadCloser.Read(p)
	b.mu.Lock()
	if !b.closed && n > 0 {
		b.timer.Reset(b.idle)
	}
	b.mu.Unlock()
	return n, e
}
func (b *idleReadCloser) Close() error {
	b.mu.Lock()
	b.closed = true
	b.timer.Stop()
	b.mu.Unlock()
	if b.cancelOnClose {
		b.cancel()
	}
	return b.ReadCloser.Close()
}
