package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/uatuko/go-imap"
	imapbe "github.com/uatuko/go-imap/backend"
	"github.com/uatuko/go-imap/backend/backendutil"
	"github.com/uatuko/go-message/textproto"

	"go.klara.mx/internal/db"
	"go.klara.mx/internal/storage"
)

const (
	mailboxHierarchydelimiter = "/"

	maxMessageSize = 1024 * 1024 * 25 // 25 MiB
)

// Message storage (overridden in tests).
var (
	newMailboxMessageReader = storage.NewMailboxMessageReader
	writeMailboxMessage     = storage.WriteMailboxMessage
)

type mailbox struct {
	ctx context.Context

	acct *db.Acct
	mbox *db.Mbox
}

func (m *mailbox) Name() string {
	return cannonicalMailboxName(m.mbox.Name())
}

func (m *mailbox) Info() (*imap.MailboxInfo, error) {
	return &imap.MailboxInfo{
		Attributes: m.mbox.Attributes(),
		Delimiter:  mailboxHierarchydelimiter,
		Name:       m.Name(),
	}, nil
}

func (m *mailbox) Status(items []imap.StatusItem) (*imap.MailboxStatus, error) {
	ctx, cancel := context.WithTimeout(m.ctx, 15*time.Second)
	defer cancel()

	// Mailboxes are cached for the session, refresh to pick up changes made since (e.g. new
	// messages delivered after login).
	if err := m.mbox.Refresh(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "[error] failed to refresh mailbox, err: %v\n", err)
		return nil, errUnknown
	}

	stats, err := m.mbox.MsgStats(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[error] failed to retrieve mailbox message stats, err: %v\n", err)
		return nil, errUnknown
	}

	s := imap.NewMailboxStatus(m.Name(), items)
	s.Flags = m.flags()
	s.PermanentFlags = append(s.Flags, "\\*")

	for _, name := range items {
		switch name {
		case imap.StatusMessages:
			s.Messages = stats.Messages
		case imap.StatusUnseen:
			// STATUS reports the number of unseen messages, SELECT reports the sequence number
			// of the first unseen message. The UNSEEN response code on SELECT is only part of
			// IMAP4rev1 (deprecated in IMAP4rev2), it's still required since the server
			// advertises IMAP4rev1.
			// Ref: https://www.rfc-editor.org/rfc/rfc9051#section-6.3.11
			// Ref: https://www.rfc-editor.org/rfc/rfc3501#section-6.3.1
			// Ref: https://www.rfc-editor.org/rfc/rfc9051#appendix-E
			s.Unseen = stats.Unseen
			s.UnseenSeqNum = stats.FirstUnseenSeq
		case imap.StatusUidNext:
			s.UidNext = m.mbox.UidNext()
		case imap.StatusUidValidity:
			s.UidValidity = m.mbox.UidValidity()
		}
	}

	return s, nil
}

func (m *mailbox) SetSubscribed(subscribed bool) error {
	return errNotImplemented
}

func (m *mailbox) Check() error {
	// No checkpoint logic to implement at this stage
	return nil
}

func (m *mailbox) ListMessages(uid bool, seqset *imap.SeqSet, items []imap.FetchItem, ch chan<- *imap.Message) error {
	defer close(ch)

	ctx, cancel := context.WithTimeout(m.ctx, 120*time.Second)
	defer cancel()

	// Fetching a body section (unless using BODY.PEEK) implicitly sets the \Seen flag, the
	// updated flags should be included in the response.
	// Ref: https://www.rfc-editor.org/rfc/rfc9051#section-6.4.5
	setSeen := false
	for _, item := range items {
		if section, err := imap.ParseBodySectionName(item); err == nil && !section.Peek {
			setSeen = true
			break
		}
	}
	if setSeen && !slices.Contains(items, imap.FetchFlags) {
		items = append(slices.Clip(items), imap.FetchFlags)
	}

	err := m.forEachMessage(ctx, uid, seqset, func(msg *db.Msg) error {
		mm, err := m.fetchMessage(ctx, msg.Seq(), items, msg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[error] failed to fetch message, err: %v\n", err)
			return nil
		}

		if setSeen && !slices.Contains(msg.Flags(), imap.SeenFlag) {
			f := append(slices.Clone(msg.Flags()), imap.SeenFlag)
			if err := msg.SetFlags(ctx, f); err != nil {
				fmt.Fprintf(os.Stderr, "[error] failed to set seen flag, err: %v\n", err)
			} else {
				mm.Flags = msg.Flags()
			}
		}

		ch <- mm
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "[error] failed to list messages, err: %v\n", err)
		return errUnknown
	}

	return nil
}

func (m *mailbox) SearchMessages(uid bool, criteria *imap.SearchCriteria) ([]uint32, error) {
	return nil, errNotImplemented
}

func (m *mailbox) CreateMessage(flags []string, date time.Time, body imap.Literal) error {
	if date.IsZero() {
		date = time.Now().UTC()
	}

	ctx, cancel := context.WithTimeout(m.ctx, 15*time.Second)
	defer cancel()

	data, err := io.ReadAll(io.LimitReader(body, maxMessageSize+1))
	if err != nil {
		fmt.Fprintf(os.Stderr, "[error] failed to read message data, err: %v\n", err)
		return errUnknown
	}

	if len(data) > maxMessageSize {
		// Required to respond with `NO [TOOBIG]`
		// Ref: https://www.rfc-editor.org/rfc/rfc4469#section-5
		return imapbe.ErrTooBig
	}

	hdr, err := textproto.ReadHeader(bufio.NewReader(bytes.NewReader(data)))
	if err != nil {
		return errInvalidMessageData
	}

	uid, err := m.mbox.IncrUidNext(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[error] failed to increment uid next, err: %v\n", err)
		return errUnknown
	}

	size, err := writeMailboxMessage(ctx, m.acct.Domain(), m.acct.Id(), m.mbox.Id(), uid, data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[error] failed to store message, err: %v\n", err)
		return errUnknown
	}

	e, _ := backendutil.FetchEnvelope(hdr)
	attrs := db.MsgAttrs{
		Envelope: db.MsgEnvelope{
			Date:      e.Date,
			Subject:   e.Subject,
			From:      mapImapToAddressList(e.From),
			Sender:    mapImapToAddressList(e.Sender),
			ReplyTo:   mapImapToAddressList(e.ReplyTo),
			To:        mapImapToAddressList(e.To),
			Cc:        mapImapToAddressList(e.Cc),
			Bcc:       mapImapToAddressList(e.Bcc),
			InReplyTo: e.InReplyTo,
			MessageId: e.MessageId,
		},
		Flags: flags,
		Size:  uint32(size),
	}

	_, err = m.mbox.NewMessage(ctx, uid, date, attrs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[error] failed to create message, err: %v\n", err)
		return errUnknown
	}

	return nil
}

func (m *mailbox) UpdateMessagesFlags(uid bool, seqset *imap.SeqSet, operation imap.FlagsOp, flags []string) error {
	ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
	defer cancel()

	err := m.forEachMessage(ctx, uid, seqset, func(msg *db.Msg) error {
		f := backendutil.UpdateFlags(msg.Flags(), operation, flags)
		if err := msg.SetFlags(ctx, f); err != nil {
			return fmt.Errorf("failed to update message flags: %w", err)
		}

		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "[error] failed to update messages flags, err: %v\n", err)
		return errUnknown
	}

	return nil
}

func (m *mailbox) CopyMessages(uid bool, seqset *imap.SeqSet, dest string) error {
	return errNotImplemented
}

func (m *mailbox) Expunge() error {
	return errNotImplemented
}

func (m *mailbox) fetchMessage(ctx context.Context, seqNum uint32, items []imap.FetchItem, msg *db.Msg) (*imap.Message, error) {
	mm := imap.NewMessage(seqNum, items)
	for _, item := range items {
		switch item {
		case imap.FetchBody, imap.FetchBodyStructure:
			r, err := newMailboxMessageReader(ctx, m.acct.Domain(), m.acct.Id(), m.mbox.Id(), msg.Uid())
			if err != nil {
				fmt.Fprintf(os.Stderr, "[error] failed create a message reader, err: %v\n", err)
				return nil, errUnknown
			}
			defer r.Close()

			data := bufio.NewReader(r)
			hdr, err := textproto.ReadHeader(data)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[error] failed to read message header, err: %v\n", err)
				return nil, errUnknown
			}

			mm.BodyStructure, err = backendutil.FetchBodyStructure(hdr, data, item == imap.FetchBodyStructure)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[error] failed to fetch message body structure, err: %v\n", err)
				return nil, errUnknown
			}

		case imap.FetchEnvelope:
			e := msg.Envelope()
			mm.Envelope = &imap.Envelope{
				Date:      e.Date,
				Subject:   e.Subject,
				From:      mapAddressListToImap(e.From),
				Sender:    mapAddressListToImap(e.Sender),
				ReplyTo:   mapAddressListToImap(e.ReplyTo),
				To:        mapAddressListToImap(e.To),
				Cc:        mapAddressListToImap(e.Cc),
				Bcc:       mapAddressListToImap(e.Bcc),
				InReplyTo: e.InReplyTo,
				MessageId: e.MessageId,
			}

		case imap.FetchFlags:
			mm.Flags = msg.Flags()
		case imap.FetchInternalDate:
			mm.InternalDate = msg.Ts()
		case imap.FetchRFC822Size:
			mm.Size = msg.Size()
		case imap.FetchUid:
			mm.Uid = msg.Uid()

		default:
			section, err := imap.ParseBodySectionName(item)
			if err != nil {
				break
			}

			r, err := newMailboxMessageReader(ctx, m.acct.Domain(), m.acct.Id(), m.mbox.Id(), msg.Uid())
			if err != nil {
				fmt.Fprintf(os.Stderr, "[error] failed create a message reader, err: %v\n", err)
				return nil, errUnknown
			}
			defer r.Close()

			data := bufio.NewReader(r)
			hdr, err := textproto.ReadHeader(data)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[error] failed to read message header, err: %v\n", err)
				return nil, errUnknown
			}

			l, _ := backendutil.FetchBodySection(hdr, data, section)
			mm.Body[section] = l
		}
	}

	return mm, nil
}

// forEachMessage calls fn, in ascending order, for each message in the mailbox matching the
// sequence set (sequence numbers or uids). Overlapping values in the set are only visited once.
func (m *mailbox) forEachMessage(ctx context.Context, uid bool, seqset *imap.SeqSet, fn func(msg *db.Msg) error) error {
	lastSeq, lastUid, err := m.mbox.LastSeqUid(ctx)
	if err != nil {
		return err
	}

	last := lastSeq
	list := m.mbox.ListMessagesBySeq
	if uid {
		last = lastUid
		list = m.mbox.ListMessagesByUidRange
	}

	if last == 0 {
		// Empty mailbox
		return nil
	}

	for _, seq := range resolveSeqSet(seqset, last).Set {
		start := seq.Start
		for {
			msgs, err := list(ctx, start, seq.Stop)
			if err != nil {
				return err
			}

			for i := range msgs {
				if err := fn(&msgs[i]); err != nil {
					return err
				}
			}

			if len(msgs) < db.ResultsLimit {
				break
			}

			// Continue from the message after the last one (in this batch)
			next := msgs[len(msgs)-1].Seq()
			if uid {
				next = msgs[len(msgs)-1].Uid()
			}

			if next >= seq.Stop {
				break
			}
			start = next + 1
		}
	}

	return nil
}

// resolveSeqSet returns a new sequence set with "*" replaced by last (the last sequence number or
// uid in the mailbox). The returned set has no dynamic values and its ranges are ordered and
// non-overlapping.
//
// e.g. with last = 3, "*" becomes "3" and "10:*" becomes "3:10".
// Ref: https://www.rfc-editor.org/rfc/rfc9051#section-6.4.9
func resolveSeqSet(seqset *imap.SeqSet, last uint32) *imap.SeqSet {
	resolved := &imap.SeqSet{}
	for _, seq := range seqset.Set {
		start, stop := seq.Start, seq.Stop
		if start == 0 {
			start = last
		}
		if stop == 0 {
			stop = last
		}

		resolved.AddRange(min(start, stop), max(start, stop))
	}

	return resolved
}

// flags returns a list of system flags applicable for this mailbox, which is the minimum
// requirement.
// Ref: https://www.rfc-editor.org/rfc/rfc9051#section-7.3.5
func (m *mailbox) flags() []string {
	return []string{
		imap.AnsweredFlag,
		imap.DeletedFlag,
		imap.DraftFlag,
		imap.FlaggedFlag,
		imap.SeenFlag,
	}
}

func cannonicalMailboxName(name string) string {
	s := strings.ToLower(name)
	switch s {
	case db.MboxNameInbox:
		// INBOX is case-insensitive and clients expect it to be named "INBOX" (e.g. `LIST "" INBOX`)
		// Ref: https://www.rfc-editor.org/rfc/rfc9051#section-5.1
		return imap.InboxName
	case db.MboxNameArchive, db.MboxNameJunk, db.MboxNameSent, db.MboxNameTrash:
		return strings.ToUpper(s[:1]) + s[1:]
	default:
		return name
	}
}

func mapAddressListToImap(addrs []db.MsgAddress) []*imap.Address {
	a := make([]*imap.Address, 0, len(addrs))
	for i := range len(addrs) {
		a = append(a, &imap.Address{
			AtDomainList: addrs[i].AtDomainList,
			HostName:     addrs[i].HostName,
			MailboxName:  addrs[i].MailboxName,
			PersonalName: addrs[i].PersonalName,
		})
	}

	return a
}

func mapImapToAddressList(imapAddrs []*imap.Address) []db.MsgAddress {
	a := make([]db.MsgAddress, 0, len(imapAddrs))
	for _, addr := range imapAddrs {
		a = append(a, db.MsgAddress{
			AtDomainList: addr.AtDomainList,
			HostName:     addr.HostName,
			MailboxName:  addr.MailboxName,
			PersonalName: addr.PersonalName,
		})
	}

	return a
}

func newMailbox(ctx context.Context, acct *db.Acct, mbox *db.Mbox) *mailbox {
	return &mailbox{
		ctx:  ctx,
		acct: acct,
		mbox: mbox,
	}
}
