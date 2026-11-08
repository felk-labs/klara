package mta

import (
	"bytes"
	"context"
	"errors"
	"strings"

	"github.com/rs/zerolog"
	"github.com/uatuko/go-message"
	"github.com/uatuko/go-message/mail"
	"github.com/uatuko/go-msgauth/dkim"

	"github.com/klara-to/mx/internal/db"
	"github.com/klara-to/mx/internal/kms"
	"github.com/klara-to/mx/internal/pb"
)

const maxTransferAttempts = 3

// Transfer communicates with the outbound MTA gRPC endpoint to transfer mail.
func Transfer(ctx context.Context, mailId string) error {
	m, err := db.RetrieveMail(ctx, mailId)
	if err != nil {
		return err
	}

	log := zerolog.Ctx(ctx).With().Str("mail_id", m.Id()).Logger()
	if m.Typ() != db.MailTypeOutbound {
		log.Warn().
			Str("mail_typ", m.Typ().String()).
			Msg("unexpected mail type, not transferring")

		return nil
	}

	if m.Status() != db.MailStatusPending {
		log.Info().
			Str("mail_status", m.Status().String()).
			Msg("mail not in pending state, not transferring")

		return nil
	}

	me, err := message.Read(bytes.NewReader(m.Data()))
	if err != nil {
		return err
	}

	_, domainName, ok := strings.Cut(m.ReturnPath(), "@")
	if !ok {
		log.Warn().Msg("invalid return path, not transferring")
		return nil
	}

	d, err := db.RetrieveDomainByName(ctx, domainName)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			log.Warn().
				Str("domain_name", domainName).
				Msg("no domain entry, not transferring")

			return nil
		}

		return err
	}

	// Populate headers
	hdr := mail.Header{Header: me.Header}
	if !hdr.Has("Message-Id") {
		hdr.GenerateMessageIDWithHostname(d.MtaHostname())
	}
	me.Header = hdr.Header

	var data bytes.Buffer
	if d.HasDomainKey() {
		var b bytes.Buffer
		if err = me.WriteTo(&b); err != nil {
			return err
		}

		if err = dkim.Sign(&data, &b, &dkim.SignOptions{
			Domain:   d.Name(),
			Selector: d.DomainKeySelector(),
			Signer:   kms.NewSigner(ctx, d.DomainKeyName(), d.DomainKeyVersion()),
		}); err != nil {
			return err
		}
	} else {
		if err = me.WriteTo(&data); err != nil {
			return err
		}
	}

	mtaClient, err := newMtaClient(ctx, d.MtaAddr(), d.MtaAud())
	if err != nil {
		return err
	}

	rcpts := m.Recipients()
	if m.FailedAttempts() > 0 {
		// Avoid retrying indefinitely
		if m.FailedAttempts() >= maxTransferAttempts {
			log.Error().
				Uint32("failed_attempts", m.FailedAttempts()).
				Msg("max transfer attempts reached, not transferring")

			if err = m.SetStatus(ctx, db.MailStatusError); err != nil {
				log.Error().Err(err).Msg("failed to update mail status to error")
			}

			return nil
		}

		// If there are failed attempts, only retry pending recipients
		rcpts = m.PendingRecipients()
	}

	if len(rcpts) == 0 {
		log.Warn().Msg("mail has no recipients, not transferring")
		return nil
	}

	rcptsMap := make(map[string][]string)
	for _, rcpt := range rcpts {
		_, domain, _ := strings.Cut(rcpt, "@")
		rcptsMap[domain] = append(rcptsMap[domain], rcpt)
	}

	if err = m.SetStatus(ctx, db.MailStatusProcessing); err != nil {
		return err
	}

	var pending []string
	for domain, rcpts := range rcptsMap {
		if _, err = mtaClient.Transfer(ctx, &pb.MtaTransferRequest{
			ReturnPath: m.ReturnPath(),
			Recipients: rcpts,
			Data:       data.Bytes(),
		}); err != nil {
			log.Error().
				Err(err).
				Str("recipient_domain", domain).
				Msg("failed to transfer mail")

			pending = append(pending, rcpts...)
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

		return errTransferFailed
	}

	if err = m.SetStatus(ctx, db.MailStatusDone); err != nil {
		// We've already transferred mail, log and ignore
		log.Warn().Err(err).Msg("failed to update mail status as done")
	}

	return nil
}
