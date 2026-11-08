package db

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	MailStatusError      MailStatus = -1
	MailStatusPending    MailStatus = 0
	MailStatusProcessing MailStatus = 1
	MailStatusDone       MailStatus = 2

	MailTypeUnknown  MailType = 0
	MailTypeInbound  MailType = 1
	MailTypeOutbound MailType = 2

	mailTypeTest MailType = -1
)

type Mail struct {
	attrs  MailAttrs
	data   []byte
	id     string
	status MailStatus
	ts     time.Time
	typ    MailType
}

func (m *Mail) Attrs() MailAttrs   { return m.attrs }
func (m *Mail) Data() []byte       { return m.data }
func (m *Mail) Id() string         { return m.id }
func (m *Mail) Status() MailStatus { return m.status }
func (m *Mail) Ts() time.Time      { return m.ts }
func (m *Mail) Typ() MailType      { return m.typ }

func (m *Mail) FailedAttempts() uint32      { return m.attrs.FailedAttempts }
func (m *Mail) PendingRecipients() []string { return m.attrs.PendingRecipients }
func (m *Mail) Recipients() []string        { return m.attrs.Envelope.To }
func (m *Mail) ReturnPath() string          { return m.attrs.Envelope.From }
func (m *Mail) Rev() int32                  { return m.attrs.Rev }

func (m *Mail) SetAttrs(ctx context.Context, attrs MailAttrs) error {
	attrs.Rev = m.Rev() + 1

	qry := `
		update mail
		set
			attrs = $3::jsonb
		where
			id = $1::text
			and attrs['_rev']::integer = $2::integer
		returning attrs['_rev']::integer;
	`

	if err := pg.QueryRow(ctx, qry, m.id, m.Rev(), attrs).Scan(&attrs.Rev); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return wrapError(err, ErrConflict)
		}

		return wrapError(err, ErrUnknown)
	}

	m.attrs = attrs
	return nil
}

func (m *Mail) SetStatus(ctx context.Context, status MailStatus) error {
	qry := `
		update mail
		set
			status = $3::integer
		where
			id = $1::text
			and attrs['_rev']::integer =  $2::integer
		returning attrs['_rev']::integer;
	`

	if err := pg.QueryRow(ctx, qry, m.id, m.Rev(), status).Scan(&m.attrs.Rev); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return wrapError(err, ErrConflict)
		}

		return wrapError(err, ErrUnknown)
	}

	m.status = status
	return nil
}

func (m *Mail) insert(ctx context.Context) error {
	m.id = uuidv7()
	m.ts = time.UnixMicro(m.ts.UnixMicro()).UTC()
	m.attrs.Rev = rand.Int32()

	qry := `
		insert into mail (
			id,
			ts,
			typ,
			status,
			data,
			attrs
		)
		values (
			$1::text,
			$2::timestamp,
			$3::integer,
			$4::integer,
			$5::bytea,
			$6::jsonb
		);
	`

	if _, err := pg.Exec(ctx, qry, m.id, m.ts, m.typ, m.status, m.data, m.attrs); err != nil {
		return err
	}

	return nil
}

type MailAttrs struct {
	// SMTP content body body encoding (e.g. 8BITMIME)
	BodyEncoding string `json:"body_encoding,omitempty"`

	// SMTP client address
	ClientAddr string `json:"client_addr,omitempty"`

	// SMTP client id used during session initiation
	ClientId string `json:"client_id,omitempty"`

	// SMTP envelope
	Envelope MailEnvelope `json:"envelope"`

	// Number of process attempts
	FailedAttempts uint32 `json:"failed_attempts,omitempty"`

	// Recipients pending transfer after the last failed attempt
	PendingRecipients []string `json:"pending_recipients,omitempty"`

	// MSA / MTA accepting mail
	ReceivedBy string `json:"received_by,omitempty"`

	Rev int32 `json:"_rev,omitempty"`
}

// Mail envelope
// Ref: https://www.rfc-editor.org/rfc/rfc5321.html#section-2.3.1
type MailEnvelope struct {
	From string   `json:"from,omitempty"`
	To   []string `json:"to,omitempty"`
}

type MailStatus int32

func (s MailStatus) String() string {
	switch s {
	case MailStatusDone:
		return "done"
	case MailStatusError:
		return "error"
	case MailStatusPending:
		return "pending"
	case MailStatusProcessing:
		return "processing"
	default:
		return "unknown"
	}
}

type MailType int32

func (t MailType) String() string {
	switch t {
	case MailTypeInbound:
		return "inbound"
	case MailTypeOutbound:
		return "outbound"
	default:
		return "unknown"
	}
}

func NewMail(ctx context.Context, ts time.Time, typ MailType, data []byte, attrs MailAttrs) (*Mail, error) {
	mail := &Mail{
		attrs: attrs,
		data:  data,
		ts:    ts,
		typ:   typ,
	}

	if err := mail.insert(ctx); err != nil {
		return nil, err
	}

	return mail, nil
}

func RetrieveMail(ctx context.Context, id string) (*Mail, error) {
	qry := `
		select
			id,
			ts,
			typ,
			status,
			data,
			attrs
		from mail
		where
			id = $1::text
		;
	`

	var mail Mail
	if err := pg.QueryRow(ctx, qry, id).Scan(
		&mail.id,
		&mail.ts,
		&mail.typ,
		&mail.status,
		&mail.data,
		&mail.attrs,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, wrapError(err, ErrNotFound)
		}

		return nil, err
	}

	return &mail, nil
}
