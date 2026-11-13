package srv

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"go.klara.mx/internal/db"
	"go.klara.mx/internal/storage"
	"go.klara.mx/pb"
)

var (
	newMailboxMessageReader = storage.NewMailboxMessageReader
)

type msgs struct {
	pb.UnimplementedMsgsServer
}

func (*msgs) RetrieveMsg(ctx context.Context, req *pb.MsgRetrieveRequest) (*pb.MsgRetrieveResponse, error) {
	msg, err := db.RetrieveMsg(ctx, req.GetId(), req.GetMboxId())
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	mbox, err := db.RetrieveMbox(ctx, req.GetMboxId())
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	acct, err := db.RetrieveAcct(ctx, mbox.AcctId())
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	r, err := newMailboxMessageReader(ctx, acct.Domain(), acct.Id(), msg.MboxId(), msg.Uid())
	if err != nil {
		fmt.Fprintf(os.Stderr, "[error] failed to create a message reader, err: %v\n", err)
		return nil, ErrUnknown
	}
	defer r.Close()

	data, err := io.ReadAll(r)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[error] failed to read message data, err: %v\n", err)
		return nil, ErrUnknown
	}

	return &pb.MsgRetrieveResponse{
		Msg: &pb.Msg{
			Id:     msg.Id(),
			MboxId: msg.MboxId(),
			Uid:    msg.Uid(),
			Data:   string(data),
		},
	}, nil
}
