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
	AttrsEventTypeKey = ".type"

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
	if ev.pbEv.Attrs == nil {
		ev.pbEv.Attrs = map[string]string{}
	}

	_, hasName := ev.pbEv.Attrs[AttrsEventNameKey]
	_, hasType := ev.pbEv.Attrs[AttrsEventTypeKey]
	if hasName && hasType {
		return ev.pbEv.Attrs
	}

	name, typ := "unknown", "unknown"
	m := ev.pbEv.ProtoReflect()
	if fd := m.WhichOneof(m.Descriptor().Oneofs().ByName("data")); fd != nil {
		name = string(fd.Name())
		typ = string(fd.Message().FullName())
	}

	if !hasName {
		ev.pbEv.Attrs[AttrsEventNameKey] = name
	}

	if !hasType {
		ev.pbEv.Attrs[AttrsEventTypeKey] = typ
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
