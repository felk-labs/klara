package main

import (
	"context"
	"errors"
	"time"

	"github.com/uatuko/go-imap"
	imapbe "github.com/uatuko/go-imap/backend"

	"github.com/klara-to/mx/internal/db"
	"github.com/klara-to/mx/valid"
)

type backend struct{}

func (be *backend) Login(conn *imap.ConnInfo, username string, password string) (imapbe.User, error) {
	if !valid.Email(username) {
		return nil, errInvalidCredentials
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	acct, err := db.RetrieveAcctByEmail(ctx, username)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, errInvalidCredentials
		}

		return nil, errUnknown
	}

	for _, cred := range acct.Credentials() {
		if cred.Verify(password) {
			ctx, cancel := context.WithCancel(context.Background())
			return &user{
				acct:   acct,
				cancel: cancel,
				ctx:    ctx,
			}, nil
		}
	}

	return nil, errInvalidCredentials
}
