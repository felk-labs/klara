package srv

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/rs/zerolog"
	m "github.com/uatuko/go-message/mail"
	"github.com/uatuko/go-message/textproto"

	"go.klara.mx/internal/db"
	"go.klara.mx/internal/events"
	pbi "go.klara.mx/internal/pb"
	"go.klara.mx/pb"
)

const (
	hdrFromKey string = "From"
	hdrToKey   string = "To"
	hdrCcKey   string = "Cc"
	hdrBccKey  string = "Bcc"

	hdrContentTypeKey string = "Content-Type"
	hdrMimeVersionKey string = "Mime-Version"

	mimeVersion string = "1.0"
)

type mail struct {
	pb.UnimplementedMailServer
}

func (*mail) Send(ctx context.Context, req *pb.MailSendRequest) (*pb.MailSendResponse, error) {
	// FIXME: we should be able to infer from actor (require authn)
	if req.From == nil {
		return nil, NewErrorf(ErrInvalidData, "no sender (require 'from')")
	}

	pbAddr := req.GetFrom()
	_, domainName, ok := strings.Cut(pbAddr.GetEmail(), "@")
	if !ok {
		return nil, NewErrorf(ErrInvalidData, "invalid sender")
	}

	_, err := db.RetrieveDomainByName(ctx, domainName)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, NewErrorf(ErrPermissionDenied, "sender not allowed")
		}

		return nil, NewError(ErrInternal, err)
	}

	attrs := db.MailAttrs{
		Envelope: db.MailEnvelope{
			From: pbAddr.GetEmail(),
		},
	}

	var hdr m.Header
	hdr.Set(hdrMimeVersionKey, mimeVersion)
	hdr.SetDate(time.Now())

	if s := req.GetSubject(); s != "" {
		hdr.SetSubject(s)
	}

	hdr.SetAddressList(hdrFromKey, []*m.Address{
		{Address: pbAddr.GetEmail(), Name: pbAddr.GetName()},
	})

	if len(req.To) > 0 {
		attrs.Envelope.To = make([]string, 0, len(req.To))
		addrs := make([]*m.Address, 0, len(req.To))
		for _, pbAddr := range req.GetTo() {
			addrs = append(addrs, &m.Address{Address: pbAddr.GetEmail(), Name: pbAddr.GetName()})
			attrs.Envelope.To = append(attrs.Envelope.To, pbAddr.Email)
		}

		hdr.SetAddressList(hdrToKey, addrs)
	}

	if len(req.Cc) > 0 {
		addrs := make([]*m.Address, 0, len(req.To))
		for _, pbAddr := range req.GetCc() {
			addrs = append(addrs, &m.Address{Address: pbAddr.GetEmail(), Name: pbAddr.GetName()})
			attrs.Envelope.To = append(attrs.Envelope.To, pbAddr.Email)
		}

		hdr.SetAddressList(hdrCcKey, addrs)
	}

	if len(attrs.Envelope.To) == 0 && len(req.Bcc) == 0 {
		return nil, NewErrorf(
			ErrInvalidData,
			"no recipients (require at least one 'to', 'cc' or 'bcc')",
		)
	}

	mail := make([]*db.Mail, 0, len(attrs.Envelope.To)+len(req.Bcc))

	if len(attrs.Envelope.To) > 0 {
		var data bytes.Buffer
		if len(req.Content) > 0 {
			if err := writeContent(&data, hdr, req.GetContent()); err != nil {
				return nil, err
			}
		} else {
			err := textproto.WriteHeader(&data, hdr.Header.Header)
			if err != nil {
				return nil, err
			}
		}

		m, err := db.NewMail(ctx, time.Now(), db.MailTypeOutbound, data.Bytes(), attrs)
		if err != nil {
			return nil, err
		}
		mail = append(mail, m)
	}

	for _, pbAddr := range req.Bcc {
		attrs.Envelope.To = []string{pbAddr.GetEmail()}

		hdr = hdr.Copy()
		hdr.SetAddressList(
			hdrBccKey,
			[]*m.Address{{Address: pbAddr.GetEmail(), Name: pbAddr.GetName()}},
		)

		var data bytes.Buffer
		if len(req.Content) > 0 {
			if err := writeContent(&data, hdr, req.GetContent()); err != nil {
				return nil, err
			}
		} else {
			err := textproto.WriteHeader(&data, hdr.Header.Header)
			if err != nil {
				return nil, err
			}
		}

		m, err := db.NewMail(ctx, time.Now(), db.MailTypeOutbound, data.Bytes(), attrs)
		if err != nil {
			return nil, err
		}
		mail = append(mail, m)
	}

	log := zerolog.Ctx(ctx)
	for _, m := range mail {
		_, err := events.Publish(ctx,
			&pbi.Event{
				Data: &pbi.Event_Mail{
					Mail: &pbi.EventDataMail{
						Id:     m.Id(),
						Status: pb.MailStatus(m.Status()),
						Type:   pb.MailType(m.Typ()),
					},
				},
			},
		)

		if err != nil {
			// Not much we can do to recover from this error, log and ignore
			log.Error().
				Err(err).
				Str("mail_id", m.Id()).
				Str("mail_status", m.Status().String()).
				Str("mail_type", m.Typ().String()).
				Msg("failed to publish mail event")
		}
	}

	return &pb.MailSendResponse{}, nil
}

func writeContent(buf *bytes.Buffer, hdr m.Header, pbContent []*pb.Message) error {
	w, err := m.CreateInlineWriter(buf, hdr)
	if err != nil {
		return err
	}

	for _, pbMsg := range pbContent {
		var h m.InlineHeader
		h.SetContentType(pbMsg.Type, nil)
		part, err := w.CreatePart(h)
		if err != nil {
			return err
		}

		io.WriteString(part, pbMsg.Data)
		part.Close()
	}

	w.Close()
	return nil
}
