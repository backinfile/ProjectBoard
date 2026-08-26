package events

import (
	"sync"
	"time"

	"github.com/projectboard/projectboard/internal/domain"
)

type Bus struct {
	mu sync.RWMutex
	nextID int64
	history []domain.Event
	subs map[chan domain.Event]struct{}
}

func New() *Bus { return &Bus{subs: make(map[chan domain.Event]struct{})} }

func (b *Bus) Publish(event domain.Event) domain.Event {
	b.mu.Lock()
	b.nextID++
	event.ID = b.nextID
	if event.At.IsZero() { event.At = time.Now().UTC() }
	b.history = append(b.history, event)
	if len(b.history) > 1000 { b.history = append([]domain.Event(nil), b.history[len(b.history)-1000:]...) }
	for ch := range b.subs {
		select { case ch <- event: default: }
	}
	b.mu.Unlock()
	return event
}

func (b *Bus) Subscribe(after int64) (<-chan domain.Event, func()) {
	ch := make(chan domain.Event, 64)
	b.mu.Lock()
	for _, event := range b.history { if event.ID > after { ch <- event } }
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() { b.mu.Lock(); if _, ok := b.subs[ch]; ok { delete(b.subs, ch); close(ch) }; b.mu.Unlock() }
}
