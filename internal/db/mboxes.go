package db

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	// Reserved mailbox names
	// Ref: https://www.rfc-editor.org/rfc/rfc9051#section-5.1

	MboxNameArchive = "archive"
	MboxNameInbox   = "inbox"
	MboxNameJunk    = "junk"
	MboxNameSent    = "sent"
	MboxNameTrash   = "trash"
)

// IMAP4rev1 mailbox
// Ref: https://www.rfc-editor.org/rfc/rfc3501
type Mbox struct {
	acctId string
	attrs  MboxAttrs
	id     string
	name   string
}

func (m *Mbox) AcctId() string { return m.acctId }
func (m *Mbox) Id() string     { return m.id }
func (m *Mbox) Name() string   { return m.name }

func (m *Mbox) Attributes() []string { return m.attrs.Attributes }
func (m *Mbox) Size() uint32         { return m.attrs.Size }
func (m *Mbox) UidNext() uint32      { return m.attrs.UidNext }
func (m *Mbox) UidValidity() uint32  { return m.attrs.UidValidity }

// NewMessage creates a new message in the mailbox. The timestamp ts is the message internal date
// (e.g. the time the message was delivered or the date given to APPEND).
// Ref: https://www.rfc-editor.org/rfc/rfc9051#section-2.3.3
func (m *Mbox) NewMessage(ctx context.Context, uid uint32, ts time.Time, attrs MsgAttrs) (*Msg, error) {
	msg := &Msg{
		attrs:  attrs,
		mboxId: m.id,
		ts:     ts,
		uid:    uid,
	}

	if err := msg.insert(ctx); err != nil {
		return nil, err
	}

	// Increment message count
	qry := `
		update mboxes
		set
			attrs['size'] = to_jsonb(coalesce(attrs['size']::integer, 0) + 1)
		where id = $1::text
		returning attrs;
	`

	if err := pg.QueryRow(ctx, qry, m.id).Scan(&m.attrs); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, wrapError(err, ErrConflict)
		}

		return nil, wrapError(err, ErrUnknown)
	}

	return msg, nil
}

// IncrUidNext increments the uid next and returns the current value
func (m *Mbox) IncrUidNext(ctx context.Context) (uint32, error) {
	qry := `
		update mboxes
		set
			attrs['uid_next'] = to_jsonb(attrs['uid_next']::integer + 1),
			attrs['_rev'] = to_jsonb(attrs['_rev']::integer + 1)
		where id = $1::text
		returning old.attrs['uid_next'], attrs;
	`

	var uid uint32
	if err := pg.QueryRow(ctx, qry, m.id).Scan(&uid, &m.attrs); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, wrapError(err, ErrConflict)
		}

		return 0, wrapError(err, ErrUnknown)
	}

	return uid, nil
}

// LastSeqUid returns the sequence number and the uid of the last message in the mailbox, or
// zeros if the mailbox is empty.
func (m *Mbox) LastSeqUid(ctx context.Context) (uint32, uint32, error) {
	qry := `
		select
			count(*)::integer,
			coalesce(max(uid), 0)::integer
		from msgs
		where mbox_id = $1::text;
	`

	var seq, uid uint32
	if err := pg.QueryRow(ctx, qry, m.id).Scan(&seq, &uid); err != nil {
		return 0, 0, wrapError(err, ErrUnknown)
	}

	return seq, uid, nil
}

// ListMessagesBySeq lists messages with sequence numbers in the inclusive range [from, to],
// up to a maximum of ResultsLimit messages.
func (m *Mbox) ListMessagesBySeq(ctx context.Context, from uint32, to uint32) ([]Msg, error) {
	if from == 0 || from > to {
		return nil, nil
	}

	qry := `
		select
			id,
			ts,
			mbox_id,
			uid,
			attrs
		from msgs
		where mbox_id = $1::text
		order by uid offset $2::bigint limit $3::bigint;
	`

	rows, err := pg.Query(ctx, qry, m.id, from-1, min(uint64(to-from)+1, ResultsLimit))
	if err != nil {
		return nil, wrapError(err, ErrUnknown)
	}
	defer rows.Close()

	msgs := make([]Msg, 0)
	for rows.Next() {
		m := Msg{seq: from + uint32(len(msgs))}
		if err := rows.Scan(&m.id, &m.ts, &m.mboxId, &m.uid, &m.attrs); err != nil {
			return nil, wrapError(err, ErrUnknown)
		}

		msgs = append(msgs, m)
	}

	if err := rows.Err(); err != nil {
		return nil, wrapError(err, ErrUnknown)
	}

	return msgs, nil
}

// ListMessagesByUidRange lists messages with uids in the inclusive range [start, end], up to a
// maximum of ResultsLimit messages.
func (m *Mbox) ListMessagesByUidRange(ctx context.Context, start uint32, end uint32) ([]Msg, error) {
	if start > end {
		return nil, nil
	}

	qry := fmt.Sprintf(`
		select
			id,
			ts,
			mbox_id,
			uid,
			attrs,
			seq
		from (
			select
				id,
				ts,
				mbox_id,
				uid,
				attrs,
				row_number() over (order by uid)::integer as seq
			from msgs
			where mbox_id = $1::text
		)
		where
			uid >= $2::bigint
			and uid <= $3::bigint
		order by uid limit %d;
	`, ResultsLimit)

	rows, err := pg.Query(ctx, qry, m.id, start, end)
	if err != nil {
		return nil, wrapError(err, ErrUnknown)
	}
	defer rows.Close()

	msgs := make([]Msg, 0)
	for rows.Next() {
		var m Msg
		if err := rows.Scan(&m.id, &m.ts, &m.mboxId, &m.uid, &m.attrs, &m.seq); err != nil {
			return nil, wrapError(err, ErrUnknown)
		}

		msgs = append(msgs, m)
	}

	if err := rows.Err(); err != nil {
		return nil, wrapError(err, ErrUnknown)
	}

	return msgs, nil
}

// MsgStats returns message statistics for the mailbox.
func (m *Mbox) MsgStats(ctx context.Context) (MboxMsgStats, error) {
	qry := `
		select
			count(*)::integer,
			(count(*) filter (where not seen))::integer,
			coalesce(min(seq) filter (where not seen), 0)::integer
		from (
			select
				row_number() over (order by uid) as seq,
				coalesce(attrs['flags'] @> jsonb_build_array($2::text), false) as seen
			from msgs
			where mbox_id = $1::text
		);
	`

	var s MboxMsgStats
	if err := pg.QueryRow(ctx, qry, m.id, MsgFlagSeen).Scan(&s.Messages, &s.Unseen, &s.FirstUnseenSeq); err != nil {
		return MboxMsgStats{}, wrapError(err, ErrUnknown)
	}

	return s, nil
}

// Refresh reloads the mailbox attributes.
func (m *Mbox) Refresh(ctx context.Context) error {
	qry := `
		select attrs
		from mboxes
		where id = $1::text;
	`

	if err := pg.QueryRow(ctx, qry, m.id).Scan(&m.attrs); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return wrapError(err, ErrNotFound)
		}

		return wrapError(err, ErrUnknown)
	}

	return nil
}

func (m *Mbox) insert(ctx context.Context) error {
	m.id = uuidv7()
	m.attrs.UidNext = 1
	m.attrs.UidValidity = uint32(time.Now().Unix())
	m.attrs.Rev = rand.Int32()
	m.populateAttributes()

	qry := `
		insert into mboxes (
			id,
			acct_id,
			name,
			attrs
		) values (
			$1::text,
			$2::text,
			$3::text,
			$4::jsonb
		);
	`

	if _, err := pg.Exec(ctx, qry, m.id, m.acctId, m.name, m.attrs); err != nil {
		return wrapError(err, ErrUnknown)
	}

	return nil
}

// populateAttributes adds RFC 6154 mailbox attributes if no attributes are set.
func (m *Mbox) populateAttributes() {
	if len(m.attrs.Attributes) > 0 {
		return
	}

	if m.attrs.Attributes == nil {
		m.attrs.Attributes = make([]string, 0, 1)
	}

	switch m.name {
	case MboxNameArchive:
		m.attrs.Attributes = append(m.attrs.Attributes, "\\Archive")
	case MboxNameJunk:
		m.attrs.Attributes = append(m.attrs.Attributes, "\\Junk")
	case MboxNameSent:
		m.attrs.Attributes = append(m.attrs.Attributes, "\\Sent")
	case MboxNameTrash:
		m.attrs.Attributes = append(m.attrs.Attributes, "\\Trash")
	}
}

type MboxAttrs struct {
	Attributes  []string `json:"attributes,omitempty"`
	Size        uint32   `json:"size,omitempty"`
	UidNext     uint32   `json:"uid_next,omitempty"`
	UidValidity uint32   `json:"uid_validity,omitempty"`

	Rev int32 `json:"_rev,omitempty"`
}

type MboxMsgStats struct {
	// Number of messages in the mailbox
	Messages uint32
	// Number of messages without the \Seen flag
	Unseen uint32
	// Sequence number of the first message without the \Seen flag, 0 if there are none
	FirstUnseenSeq uint32
}

func ListMboxesByAcctId(ctx context.Context, acctId string) ([]Mbox, error) {
	qry := `
		select
			id,
			acct_id,
			name,
			attrs
		from mboxes
		where acct_id = $1::text;
	`

	rows, err := pg.Query(ctx, qry, acctId)
	if err != nil {
		return nil, wrapError(err, ErrUnknown)
	}

	mboxes := make([]Mbox, 0)
	for rows.Next() {
		var m Mbox
		if err := rows.Scan(&m.id, &m.acctId, &m.name, &m.attrs); err != nil {
			return nil, wrapError(err, ErrUnknown)
		}

		mboxes = append(mboxes, m)
	}

	return mboxes, nil
}

func NewMbox(ctx context.Context, acctId string, name string, attrs MboxAttrs) (*Mbox, error) {
	m := &Mbox{
		acctId: acctId,
		attrs:  attrs,
		name:   cannonicalMboxName(name),
	}

	if err := m.insert(ctx); err != nil {
		return nil, err
	}

	return m, nil
}

func NewPrimaryMbox(ctx context.Context, acctId string) (*Mbox, error) {
	return NewMbox(ctx, acctId, MboxNameInbox, MboxAttrs{})
}

func RetrieveMbox(ctx context.Context, id string) (*Mbox, error) {
	qry := `
		select
			id,
			acct_id,
			name,
			attrs
		from mboxes
		where
			id = $1::text
		;
	`

	var mbox Mbox
	if err := pg.QueryRow(ctx, qry, id).Scan(
		&mbox.id,
		&mbox.acctId,
		&mbox.name,
		&mbox.attrs,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, wrapError(err, ErrNotFound)
		}

		return nil, err
	}

	return &mbox, nil
}

func RetrieveMboxByAcctId(ctx context.Context, acctId string, name string) (*Mbox, error) {
	qry := `
		select
			id,
			acct_id,
			name,
			attrs
		from mboxes
		where
			acct_id = $1::text
			and name = $2::text
		;
	`

	var mbox Mbox
	if err := pg.QueryRow(ctx, qry, acctId, cannonicalMboxName(name)).Scan(
		&mbox.id,
		&mbox.acctId,
		&mbox.name,
		&mbox.attrs,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, wrapError(err, ErrNotFound)
		}

		return nil, err
	}

	return &mbox, nil
}

func cannonicalMboxName(name string) string {
	s := strings.ToLower(name)
	switch s {
	case MboxNameArchive, MboxNameInbox, MboxNameJunk, MboxNameSent, MboxNameTrash:
		return s
	default:
		return name
	}
}
