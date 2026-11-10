package main

import (
	"github.com/uatuko/go-smtp"

	"go.klara.mx/internal/mta"
)

type backend struct{}

func (b *backend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	return mta.NewAuthSession(c)
}
