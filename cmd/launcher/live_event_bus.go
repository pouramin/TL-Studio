package main

import (
	"sync"
)

type liveEventBus struct {
	mu          sync.Mutex
	nextID      uint64
	subscribers map[uint64]chan liveEventView
}

func newLiveEventBus() *liveEventBus {
	return &liveEventBus{subscribers: map[uint64]chan liveEventView{}}
}

func (b *liveEventBus) publish(event liveEventView) {
	if b == nil || event.Type == "" {
		return
	}
	event.Version = liveEventContractVersion
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, subscriber := range b.subscribers {
		select {
		case subscriber <- event:
		default:
		}
	}
}

func (b *liveEventBus) subscribe() (uint64, <-chan liveEventView) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextID++
	id := b.nextID
	channel := make(chan liveEventView, 64)
	b.subscribers[id] = channel
	return id, channel
}

func (b *liveEventBus) unsubscribe(id uint64) {
	if b == nil {
		return
	}
	b.mu.Lock()
	channel, ok := b.subscribers[id]
	if ok {
		delete(b.subscribers, id)
		close(channel)
	}
	b.mu.Unlock()
}
