package mda

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/uatuko/go-message"
	"github.com/uatuko/go-message/mail"

	"go.klara.mx/internal/db"
	"go.klara.mx/internal/storage"
)

// Deliver handles local delivery of inbound mail by storing it in the recipients' inboxes.
func Deliver(ctx context.Context, mailId string) error {
	m, err := db.RetrieveMail(ctx, mailId)
	if err != nil {
		return err
	}

	log := zerolog.Ctx(ctx).With().Str("mail_id", m.Id()).Logger()
	if m.Typ() != db.MailTypeInbound {
		log.Warn().
			Str("mail_typ", m.Typ().String()).
			Msg("unexpected mail type, not delivering")

		return nil
	}

	if m.Status() != db.MailStatusPending {
		log.Info().
			Str("mail_status", m.Status().String()).
			Msg("mail not in pending state, not delivering")

		return nil
	}

	// If there are failed attempts, only retry pending recipients
	rcpts := m.Recipients()
	if m.FailedAttempts() > 0 {
		rcpts = m.PendingRecipients()
	}

	if len(rcpts) == 0 {
		log.Warn().Msg("mail has no recipients, not delivering")
		return nil
	}

	me, err := message.Read(bytes.NewReader(m.Data()))
	if err != nil {
		// Not possible to recover, log and ignore
		log.Error().Err(err).Msg("failed to read message, not delivering")
		return nil
	}

	msgAttrs := db.MsgAttrs{
		Envelope: msgEnvelope(me),
	}

	if err = m.SetStatus(ctx, db.MailStatusProcessing); err != nil {
		return err
	}

	var pending []string
	for _, rcpt := range rcpts {
		if err = deliver(ctx, rcpt, m.Data(), msgAttrs); err != nil {
			if errors.Is(err, db.ErrNotFound) {
				log.Warn().
					Str("recipient", rcpt).
					Msg("no account or mailbox found for recipient, skipping delivery")

				continue
			}

			log.Error().
				Err(err).
				Str("recipient", rcpt).
				Msg("failed to deliver mail to recipient")

			pending = append(pending, rcpt)
		}
	}

	if len(pending) > 0 {
		// There were failures, queue for retry
		attrs := m.Attrs()
		attrs.FailedAttempts++
		attrs.PendingRecipients = pending
		if err = m.SetAttrs(ctx, attrs); err != nil {
			// Not possible to recover, log and ignore
			log.Error().Err(err).Msg("failed to update mail attrs")
			return nil
		}

		if err = m.SetStatus(ctx, db.MailStatusPending); err != nil {
			// Not possible to recover, log and ignore
			log.Error().
				Err(err).
				Msg("failed to rollback mail status to pending")

			return nil
		}

		return errDeliveryFailed
	}

	if err = m.SetStatus(ctx, db.MailStatusDone); err != nil {
		// We've already delivered mail, log and ignore
		log.Warn().Err(err).Msg("failed to update mail status as done")
	}

	return nil
}

func deliver(ctx context.Context, rcpt string, data []byte, msgAttrs db.MsgAttrs) error {
	acct, err := db.RetrieveAcctByEmail(ctx, rcpt)
	if err != nil {
		return err
	}

	inbox, err := db.RetrieveMboxByAcctId(ctx, acct.Id(), db.MboxNameInbox)
	if err != nil {
		return err
	}

	uid, err := inbox.IncrUidNext(ctx)
	if err != nil {
		return err
	}

	size, err := storage.WriteMailboxMessage(ctx, acct.Domain(), acct.Id(), inbox.Id(), uid, data)
	if err != nil {
		return err
	}

	msgAttrs.Size = uint32(size)
	_, err = inbox.NewMessage(ctx, uid, time.Now(), msgAttrs)
	return err
}

func msgAddressList(hdr *mail.Header, field string) []db.MsgAddress {
	addrs, err := hdr.AddressList(field)
	if err != nil || len(addrs) == 0 {
		return nil
	}

	msgAddrs := make([]db.MsgAddress, 0, len(addrs))
	for _, addr := range addrs {
		mailbox, hostname, _ := strings.Cut(addr.Address, "@")
		msgAddrs = append(msgAddrs, db.MsgAddress{
			PersonalName: addr.Name,
			MailboxName:  mailbox,
			HostName:     hostname,
		})
	}

	return msgAddrs
}

func msgEnvelope(me *message.Entity) db.MsgEnvelope {
	var e db.MsgEnvelope
	hdr := &mail.Header{Header: me.Header}

	if date, err := hdr.Date(); err == nil {
		e.Date = date
	} else {
		e.Date = time.Now()
	}

	if subject, err := hdr.Subject(); err == nil {
		e.Subject = subject
	}

	e.From = msgAddressList(hdr, "From")
	e.Sender = msgAddressList(hdr, "Sender")
	e.ReplyTo = msgAddressList(hdr, "Reply-To")
	e.To = msgAddressList(hdr, "To")
	e.Cc = msgAddressList(hdr, "Cc")
	e.Bcc = msgAddressList(hdr, "Bcc")
	e.InReplyTo = hdr.Get("In-Reply-To")
	e.MessageId = hdr.Get("Message-Id")

	return e
}
