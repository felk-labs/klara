package events

import (
	"context"

	"github.com/klara-to/mx/internal/pb"
)

var publisher Publisher

type Publisher interface {
	Publish(context.Context, *Event) error
	PublishSync(context.Context, *Event) error

	Stop()
}

func Publish(ctx context.Context, pbEv *pb.Event) (*Event, error) {
	ev := newEvent(pbEv)
	if err := publisher.Publish(ctx, ev); err != nil {
		return nil, err
	}

	return ev, nil
}

func PublishSync(ctx context.Context, pbEv *pb.Event) (*Event, error) {
	ev := newEvent(pbEv)
	if err := publisher.PublishSync(ctx, ev); err != nil {
		return nil, err
	}

	return ev, nil
}
