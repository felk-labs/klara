package main

import (
	"github.com/uatuko/go-smtp"

	"go.klara.mx/internal/mda"
)

type backend struct {
	catchAll bool
}

func (b *backend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	return mda.NewSession(c, mda.SessionOpts{CatchAll: b.catchAll})
}
