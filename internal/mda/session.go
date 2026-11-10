package mda

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/uatuko/go-smtp"

	"go.klara.mx/internal/db"
	"go.klara.mx/internal/events"
	pbi "go.klara.mx/internal/pb"
	"go.klara.mx/pb"
)

type SessionOpts struct {
	CatchAll bool
}

type session struct {
	bodyEncoding smtp.BodyType
	from         string
	to           []string

	conn *smtp.Conn
	opts SessionOpts
}

func (s *session) Data(r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(
		log.Logger.WithContext(context.TODO()), 15*time.Second)
	defer cancel()

	m, err := db.NewMail(
		ctx,
		time.Now(),
		db.MailTypeInbound,
		data,
		db.MailAttrs{
			BodyEncoding: string(s.bodyEncoding),
			Envelope: db.MailEnvelope{
				From: s.from,
				To:   s.to,
			},
			ClientAddr: s.conn.Conn().RemoteAddr().String(),
			ClientId:   s.conn.Hostname(),
			ReceivedBy: s.conn.Server().Domain,
		},
	)
	if err != nil {
		log.Error().Err(err).Msg("failed to store mail")
		return errInternal
	}

	_, err = events.Publish(ctx,
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
		log.Error().
			Err(err).
			Str("mail_id", m.Id()).
			Msg("failed to publish mail event for delivery")
	}

	return nil
}

func (s *session) Logout() error {
	return nil
}

func (s *session) Mail(from string, opts *smtp.MailOptions) error {
	s.bodyEncoding = opts.Body
	s.from = from
	return nil
}

func (s *session) Rcpt(to string, opts *smtp.RcptOptions) error {
	to = strings.ToLower(to)
	if !s.opts.CatchAll {
		_, domain, _ := strings.Cut(to, "@")
		_, err := db.RetrieveDomainByName(context.TODO(), domain)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				return errInvalidRecipientDomain
			}

			log.Error().Err(err).Msg("failed to retrieve domain")
			return errInternal
		}
	}

	if s.to == nil {
		s.to = make([]string, 0, 1)
	}
	s.to = append(s.to, to)

	return nil
}

func (s *session) Reset() {
	s.bodyEncoding = ""
	s.from = ""
	s.to = nil
}

func NewSession(c *smtp.Conn, opts SessionOpts) (smtp.Session, error) {
	return &session{
		conn: c,
		opts: opts,
	}, nil
}
