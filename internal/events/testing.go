//go:build test

package events

import (
	"context"
	"sync"
)

type mockPublisher struct {
	listeners []chan *Event

	sync.Mutex
}

func (p *mockPublisher) Publish(ctx context.Context, ev *Event) error {
	go func() {
		p.PublishSync(ctx, ev)
	}()

	return nil
}

func (p *mockPublisher) PublishSync(ctx context.Context, ev *Event) error {
	p.Lock()
	for _, ch := range p.listeners {
		ch <- ev
	}
	p.Unlock()

	return nil
}

func (p *mockPublisher) Stop() {
	for _, ch := range p.listeners {
		close(ch)
	}
}

func (p *mockPublisher) listen(ch chan *Event) {
	p.Lock()
	p.listeners = append(p.listeners, ch)
	p.Unlock()
}

func NewListener() <-chan *Event {
	ch := make(chan *Event, 1)
	publisher.(*mockPublisher).listen(ch)

	return ch
}

func Setup() error {
	publisher = &mockPublisher{
		listeners: make([]chan *Event, 0),
	}

	return nil
}

func Teardown() {
	publisher.Stop()
}
