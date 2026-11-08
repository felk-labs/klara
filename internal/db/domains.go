package db

import (
	"context"
	"errors"
	"math/rand/v2"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/net/idna"
)

type Domain struct {
	attrs DomainAttrs
	id    string
	name  string
}

func (d *Domain) Attrs() DomainAttrs { return d.attrs }
func (d *Domain) Id() string         { return d.id }
func (d *Domain) Name() string       { return d.name }

func (d *Domain) DomainKeyName() string     { return d.attrs.DomainKeyName }
func (d *Domain) DomainKeySelector() string { return d.attrs.DomainKeySelector }
func (d *Domain) DomainKeyVersion() string  { return d.attrs.DomainKeyVersion }

func (d *Domain) MtaAddr() string     { return d.attrs.MtaAddr }
func (d *Domain) MtaAud() string      { return d.attrs.MtaAud }
func (d *Domain) MtaHostname() string { return d.attrs.MtaHostname }

func (d *Domain) Rev() int32 { return d.attrs.Rev }

func (d *Domain) HasDomainKey() bool {
	return d.attrs.DomainKeyName != "" &&
		d.attrs.DomainKeyVersion != "" &&
		d.attrs.DomainKeySelector != ""
}

func (d *Domain) insert(ctx context.Context) error {
	if err := d.validate(); err != nil {
		return err
	}

	d.id = uuidv7()
	d.attrs.Rev = rand.Int32()

	qry := `
		insert into domains (
			id,
			name,
			attrs
		)
		values (
			$1::text,
			$2::text,
			$3::jsonb
		);
	`

	if _, err := pg.Exec(ctx, qry, d.id, d.name, d.attrs); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			switch pgErr.Code {
			case "23505": // unique violation
				return wrapError(err, ErrAlreadyExists)
			}
		}

		return wrapError(err, ErrUnknown)
	}

	return nil
}

func (d *Domain) validate() error {
	name, err := idna.ToASCII(strings.ToLower(d.name))
	if err != nil {
		return wrapError(err, ErrInvalidData)
	}

	if !rxDomainName().MatchString(name) {
		return wrapError(errors.New("invalid domain name"), ErrInvalidData)
	}
	d.name = name

	return nil
}

type DomainAttrs struct {
	DomainKeyName     string `json:"domain_key_name,omitempty"`
	DomainKeySelector string `json:"domain_key_selector,omitempty"`
	DomainKeyVersion  string `json:"domain_key_version,omitempty"`

	// MTA gRPC address
	MtaAddr string `json:"mta_addr,omitempty"`

	// MTA authentication token audience
	MtaAud string `json:"mta_aud,omitempty"`

	// MTA hostname
	MtaHostname string `json:"mta_hostname,omitempty"`

	Rev int32 `json:"_rev,omitempty"`
}

func NewDomain(ctx context.Context, name string, attrs DomainAttrs) (*Domain, error) {
	d := &Domain{
		attrs: attrs,
		name:  name,
	}

	if err := d.insert(ctx); err != nil {
		return nil, err
	}

	return d, nil
}

func RetrieveDomain(ctx context.Context, id string) (*Domain, error) {
	qry := `
		select
			id,
			name,
			attrs
		from domains
		where
			id = $1::text
		;
	`

	var d Domain
	if err := pg.QueryRow(ctx, qry, id).Scan(
		&d.id,
		&d.name,
		&d.attrs,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, wrapError(err, ErrNotFound)
		}

		return nil, err
	}

	return &d, nil
}

func RetrieveDomainByName(ctx context.Context, name string) (*Domain, error) {
	qry := `
		select
			id,
			name,
			attrs
		from domains
		where
			name = $1::text
		;
	`

	var d Domain
	if err := pg.QueryRow(ctx, qry, name).Scan(
		&d.id,
		&d.name,
		&d.attrs,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, wrapError(err, ErrNotFound)
		}

		return nil, err
	}

	return &d, nil
}
