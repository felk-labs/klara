package main

import (
	"github.com/klara-to/mx/internal/mta"
	"github.com/uatuko/go-smtp"
)

type backend struct{}

func (b *backend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	return mta.NewAuthSession(c)
}
