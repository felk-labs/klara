package main

import (
	"github.com/klara-to/mx/internal/mda"
	"github.com/uatuko/go-smtp"
)

type backend struct {
	catchAll bool
}

func (b *backend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	return mda.NewSession(c, mda.SessionOpts{CatchAll: b.catchAll})
}
