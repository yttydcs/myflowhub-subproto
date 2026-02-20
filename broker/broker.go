package broker

import (
	"strings"
	"sync"
)

// Broker 是进程内的 reqID -> 响应 投递器。
//
// 说明：
// - 这是“同进程等待/唤醒”，不是网络 pending；网络响应仍通过 Core 路由到达目标节点后再进入该投递器。
// - chan 为缓冲 1，避免 Deliver 侧阻塞；Deliver 会关闭通道以提示完成。
type Broker[T any] struct {
	mu      sync.Mutex
	waiters map[string]chan T
}

func New[T any]() *Broker[T] {
	return &Broker[T]{waiters: make(map[string]chan T)}
}

func (b *Broker[T]) Register(reqID string) (ch <-chan T, cancel func()) {
	reqID = strings.TrimSpace(reqID)
	out := make(chan T, 1)
	if reqID == "" {
		close(out)
		return out, func() {}
	}
	b.mu.Lock()
	if b.waiters == nil {
		b.waiters = make(map[string]chan T)
	}
	// 防御性：若重复注册同 reqID，关闭旧通道避免泄漏等待者。
	if prev, ok := b.waiters[reqID]; ok {
		delete(b.waiters, reqID)
		close(prev)
	}
	b.waiters[reqID] = out
	b.mu.Unlock()

	return out, func() {
		b.mu.Lock()
		if c, ok := b.waiters[reqID]; ok {
			delete(b.waiters, reqID)
			close(c)
		}
		b.mu.Unlock()
	}
}

func (b *Broker[T]) Deliver(reqID string, resp T) bool {
	reqID = strings.TrimSpace(reqID)
	if reqID == "" {
		return false
	}
	b.mu.Lock()
	ch, ok := b.waiters[reqID]
	if ok {
		delete(b.waiters, reqID)
	}
	b.mu.Unlock()
	if !ok {
		return false
	}
	ch <- resp
	close(ch)
	return true
}
