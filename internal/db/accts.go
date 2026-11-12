package db

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Acct struct {
	attrs  AcctAttrs
	domain string
	id     string
	name   string
}

func (a *Acct) Attrs() AcctAttrs { return a.attrs }
func (a *Acct) Domain() string   { return a.domain }
func (a *Acct) Id() string       { return a.id }
func (a *Acct) Name() string     { return a.name }
func (a *Acct) Rev() int32       { return a.attrs.Rev }

func (a *Acct) Credentials() []Credential { return a.attrs.Credentials }

func (a *Acct) Email() string {
	return fmt.Sprintf("%s@%s", a.name, a.domain)
}

func (a *Acct) insert(ctx context.Context) error {
	if err := a.validate(); err != nil {
		return err
	}

	a.id = uuidv7()
	a.attrs.Rev = rand.Int32()

	qry := `
		insert into accts (
			id,
			domain,
			name,
			attrs
		) values (
			$1::text,
			$2::text,
			$3::text,
			$4::jsonb
		);
	`

	if _, err := pg.Exec(ctx, qry, a.id, a.domain, a.name, a.attrs); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			switch pgErr.Code {
			case "23503": // fkey violation
				return wrapError(err, ErrInvalidData)
			case "23505": // unique violation
				return wrapError(err, ErrAlreadyExists)
			}
		}

		return wrapError(err, ErrUnknown)
	}

	return nil
}

func (a *Acct) validate() error {
	name := strings.ToLower(a.name)
	if !rxAccountName().MatchString(name) {
		return wrapError(errors.New("invalid account name"), ErrInvalidData)
	}
	a.name = name

	return nil
}

type AcctAttrs struct {
	Credentials []Credential `json:"creds,omitempty"`

	Rev int32 `json:"_rev,omitempty"`
}

func NewAcct(ctx context.Context, domain string, name string, attrs AcctAttrs) (*Acct, error) {
	a := &Acct{
		attrs:  attrs,
		domain: domain,
		name:   name,
	}

	if err := a.insert(ctx); err != nil {
		return nil, err
	}

	return a, nil
}

func RetrieveAcct(ctx context.Context, id string) (*Acct, error) {
	qry := `
		select
			id,
			domain,
			name,
			attrs
		from accts
		where
			id = $1::text
		;
	`

	var acct Acct
	if err := pg.QueryRow(ctx, qry, id).Scan(
		&acct.id,
		&acct.domain,
		&acct.name,
		&acct.attrs,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, wrapError(err, ErrNotFound)
		}

		return nil, err
	}

	return &acct, nil
}

func RetrieveAcctByEmail(ctx context.Context, email string) (*Acct, error) {
	// Account names and domains are stored in lowercase
	name, domain, ok := strings.Cut(strings.ToLower(email), "@")
	if !ok {
		return nil, ErrNotFound
	}

	qry := `
		select
			id,
			domain,
			name,
			attrs
		from accts
		where
			domain = $1::text
			and name = $2::text
		;
	`

	var acct Acct
	if err := pg.QueryRow(ctx, qry, domain, name).Scan(
		&acct.id,
		&acct.domain,
		&acct.name,
		&acct.attrs,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, wrapError(err, ErrNotFound)
		}

		return nil, err
	}

	return &acct, nil
}
