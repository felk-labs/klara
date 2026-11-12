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

type mailboxes struct {
	pb.UnimplementedMailboxesServer
}

func (*mailboxes) Retrieve(ctx context.Context, req *pb.MailboxMessageRetrieveRequest) (*pb.MailboxMessageRetrieveResponse, error) {
	msg, err := db.RetrieveMsg(ctx, req.GetId(), req.GetMailboxId())
	if err != nil {
		mapDbErr(err)
	}

	mbox, err := db.RetrieveMbox(ctx, req.GetMailboxId())
	if err != nil {
		mapDbErr(err)
	}

	acct, err := db.RetrieveAcct(ctx, mbox.AcctId())
	if err != nil {
		mapDbErr(err)
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

	return &pb.MailboxMessageRetrieveResponse{
		Msg: &pb.MailboxMessage{
			Id:        msg.Id(),
			MailboxId: msg.MboxId(),
			Uid:       msg.Uid(),
			Raw:       data,
		},
	}, nil
}

func mapDbErr(err error) error {
	if errors.Is(err, db.ErrInvalidData) {
		return ErrInvalidData
	}
	if errors.Is(err, db.ErrNotFound) {
		return ErrNotFound
	}
	return err
}
