package gateway

import (
	"fmt"
	"io"
	"sync/atomic"
	"time"
)

// idleTimeoutReader 在 timeout 内没有读到任何数据时关闭底层 body，
// 解除阻塞中的 Read 并让调用方收到超时错误。
type idleTimeoutReader struct {
	body     io.ReadCloser
	reset    chan struct{}
	done     chan struct{}
	timedOut atomic.Bool
	timeout  time.Duration
}

func newIdleTimeoutReader(body io.ReadCloser, timeout time.Duration) *idleTimeoutReader {
	reader := &idleTimeoutReader{body: body, reset: make(chan struct{}, 1), done: make(chan struct{}), timeout: timeout}
	go reader.watch()
	return reader
}

func (r *idleTimeoutReader) watch() {
	timer := time.NewTimer(r.timeout)
	defer timer.Stop()
	for {
		select {
		case <-r.done:
			return
		case <-r.reset:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(r.timeout)
		case <-timer.C:
			r.timedOut.Store(true)
			r.body.Close()
			return
		}
	}
}

func (r *idleTimeoutReader) Read(p []byte) (int, error) {
	if r.timedOut.Load() {
		return 0, fmt.Errorf("upstream stream idle for over %s", r.timeout)
	}
	n, err := r.body.Read(p)
	if r.timedOut.Load() {
		return n, fmt.Errorf("upstream stream idle for over %s", r.timeout)
	}
	if err == nil && n > 0 {
		select {
		case r.reset <- struct{}{}:
		default:
		}
	}
	return n, err
}

func (r *idleTimeoutReader) Close() error {
	close(r.done)
	return r.body.Close()
}
