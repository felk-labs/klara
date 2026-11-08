package srv

import (
	"context"
	"errors"

	"github.com/klara-to/mx/internal/db"
	"github.com/klara-to/mx/pb"
)

type accounts struct {
	pb.UnimplementedAccountsServer
}

func (*accounts) Create(ctx context.Context, req *pb.AccountsCreateRequest) (*pb.AccountsCreateResponse, error) {
	attrs := db.AcctAttrs{
		Credentials: make([]db.Credential, 0),
	}
	if req.Password != nil {
		cred := db.NewCredentialArgon2([]byte(req.GetPassword()))
		attrs.Credentials = append(attrs.Credentials, cred)
	}

	acct, err := db.NewAcct(ctx, req.GetDomain(), req.GetName(), attrs)
	if err != nil {
		if errors.Is(err, db.ErrInvalidData) {
			return nil, ErrInvalidData
		}
		if errors.Is(err, db.ErrAlreadyExists) {
			return nil, ErrAlreadyExists
		}

		return nil, err
	}

	// Create primary mailbox for the account
	if _, err = db.NewPrimaryMbox(ctx, acct.Id()); err != nil {
		return nil, err
	}

	return &pb.AccountsCreateResponse{
		Account: mapAcctToPb(acct),
	}, nil
}

func mapAcctToPb(a *db.Acct) *pb.Account {
	return &pb.Account{
		Id:     a.Id(),
		Domain: a.Domain(),
		Name:   a.Name(),
	}
}
