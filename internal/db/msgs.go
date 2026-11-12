package db

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	// Ref: https://www.rfc-editor.org/rfc/rfc9051#section-2.3.2
	MsgFlagSeen = "\\Seen"
)

type Reader interface {
	Read(ctx context.Context, ref string) ([]byte, error)
}

type Msg struct {
	attrs  MsgAttrs
	id     string
	mboxId string
	ts     time.Time
	uid    uint32

	// Message sequence number, only populated when listing messages from a mailbox.
	// Ref: https://www.rfc-editor.org/rfc/rfc9051#section-2.3.1.2
	seq uint32

	data []byte
}

func (m *Msg) Attrs() MsgAttrs { return m.attrs }
func (m *Msg) Id() string      { return m.id }
func (m *Msg) MboxId() string  { return m.mboxId }
func (m *Msg) Seq() uint32     { return m.seq }
func (m *Msg) Ts() time.Time   { return m.ts }
func (m *Msg) Uid() uint32     { return m.uid }

func (m *Msg) Envelope() MsgEnvelope { return m.attrs.Envelope }
func (m *Msg) Flags() []string       { return m.attrs.Flags }
func (m *Msg) Size() uint32          { return m.attrs.Size }

func (m *Msg) Rev() int32 { return m.attrs.Rev }

func (m *Msg) SetFlags(ctx context.Context, flags []string) error {
	qry := `
		update msgs
		set
			attrs['flags'] = $3::jsonb,
			attrs['_rev'] = to_jsonb(attrs['_rev']::integer + 1)
		where
			id = $1::text
			and attrs['_rev']::integer =  $2::integer
		returning attrs['_rev']::integer;
	`

	if err := pg.QueryRow(ctx, qry, m.id, m.Rev(), flags).Scan(&m.attrs.Rev); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return wrapError(err, ErrConflict)
		}

		return wrapError(err, ErrUnknown)
	}

	m.attrs.Flags = flags
	return nil
}

func (m *Msg) insert(ctx context.Context) error {
	m.id = uuidv7()
	m.ts = time.UnixMicro(m.ts.UnixMicro()).UTC()
	m.attrs.Rev = rand.Int32()

	qry := `
		insert into msgs (
			id,
			ts,
			mbox_id,
			uid,
			attrs
		) values (
			$1::text,
			$2::timestamp,
			$3::text,
			$4::integer,
			$5::jsonb
		);
	`

	if _, err := pg.Exec(ctx, qry, m.id, m.ts, m.mboxId, m.uid, m.attrs); err != nil {
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

func RetrieveMsg(ctx context.Context, id string, mbox_id string) (*Msg, error) {
	qry := `
		select
			id,
			ts,
			mbox_id,
			uid,
			attrs
		from msgs
		where
			id = $1::text
			and mbox_id = $2::text
		;
	`

	var d Msg
	if err := pg.QueryRow(ctx, qry, id, mbox_id).Scan(
		&d.id,
		&d.ts,
		&d.mboxId,
		&d.uid,
		&d.attrs,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, wrapError(err, ErrNotFound)
		}

		return nil, err
	}

	return &d, nil
}

// Electronic mail address
// Ref: https://www.rfc-editor.org/rfc/rfc9051#section-7.5.2
type MsgAddress struct {
	// The personal name.
	PersonalName string `json:"personal-name,omitempty"`
	// The SMTP at-domain-list (source route).
	AtDomainList string `json:"at-domain-list,omitempty"`
	// The mailbox name.
	MailboxName string `json:"mailbox,omitempty"`
	// The host name.
	HostName string `json:"hostname,omitempty"`
}

type MsgAttrs struct {
	Envelope MsgEnvelope `json:"envelope"`
	Flags    []string    `json:"flags,omitempty"`
	Size     uint32      `json:"size"`

	Rev int32 `json:"_rev,omitempty"`
}

// Message envelope
// Ref: https://www.rfc-editor.org/rfc/rfc9051#section-7.5.2
type MsgEnvelope struct {
	Date    time.Time `json:"date,omitzero"`
	Subject string    `json:"subject,omitempty"`

	From    []MsgAddress `json:"from,omitempty"`
	Sender  []MsgAddress `json:"sender,omitempty"`
	ReplyTo []MsgAddress `json:"reply-to,omitempty"`
	To      []MsgAddress `json:"to,omitempty"`
	Cc      []MsgAddress `json:"cc,omitempty"`
	Bcc     []MsgAddress `json:"bcc,omitempty"`

	// The In-Reply-To header, should contain the parent Message-Id
	// Ref: https://www.rfc-editor.org/rfc/rfc5322#section-3.6.4
	InReplyTo string `json:"in-reply-to,omitempty"`

	// The Message-Id header
	MessageId string `json:"message-id,omitempty"`
}
