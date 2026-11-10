package events

import (
	"context"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"go.klara.mx/internal/pb"
)

const (
	AttrsEventNameKey = ".name"

	publisherWaitTimeout = 10 * time.Second
)

type Event struct {
	pbEv *pb.Event
}

func (ev *Event) Marshal() ([]byte, error) {
	if !ev.pbEv.Timestamp.IsValid() {
		ev.pbEv.Timestamp = timestamppb.New(time.Now().UTC())
	}

	return proto.Marshal(ev.pbEv)
}

func (ev *Event) Unwrap() *pb.Event {
	return ev.pbEv
}

func (ev *Event) attrs() map[string]string {
	_, ok := ev.pbEv.Attrs[AttrsEventNameKey]
	if ok {
		return ev.pbEv.Attrs
	}

	var name string
	switch ev.pbEv.Data.(type) {
	case *pb.Event_Mail:
		name = "mail"
	default:
		name = "unknown"
	}

	if ev.pbEv.Attrs == nil {
		ev.pbEv.Attrs = map[string]string{
			AttrsEventNameKey: name,
		}
	} else {
		ev.pbEv.Attrs[AttrsEventNameKey] = name
	}

	return ev.pbEv.Attrs
}

func Init(ctx context.Context, projectId string, topic string) error {
	err := initPubsub(ctx, projectId, topic)
	if err != nil {
		return err
	}

	return nil
}

func Stop() {
	stopPubsub()
}

func Unmarshal(b []byte) (*Event, error) {
	var pbEv pb.Event
	err := proto.Unmarshal(b, &pbEv)
	if err != nil {
		return nil, err
	}

	return &Event{
		pbEv: &pbEv,
	}, nil
}

func newEvent(pbEv *pb.Event) *Event {
	return &Event{
		pbEv: pbEv,
	}
}
