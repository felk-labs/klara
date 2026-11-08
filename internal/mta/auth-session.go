package mta

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/uatuko/go-sasl"
	"github.com/uatuko/go-smtp"

	"github.com/klara-to/mx/internal/db"
	"github.com/klara-to/mx/internal/events"
	pbi "github.com/klara-to/mx/internal/pb"
	"github.com/klara-to/mx/pb"
)

type authSession struct {
	bodyEncoding smtp.BodyType
	from         string
	to           []string

	acct *db.Acct
	conn *smtp.Conn
}

func (s *authSession) Auth(m string) (sasl.Server, error) {
	if m != sasl.Plain {
		return nil, smtp.ErrAuthUnknownMechanism
	}

	return sasl.NewPlainServer(func(identity, username, password string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		acct, err := db.RetrieveAcctByEmail(ctx, username)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				return smtp.ErrAuthFailed
			}

			return errInternal
		}

		for _, cred := range acct.Credentials() {
			if cred.Verify(password) {
				s.acct = acct
				return nil
			}
		}

		return smtp.ErrAuthFailed
	}), nil
}

func (s *authSession) AuthMechanisms() []string {
	return []string{sasl.Plain}
}

func (s *authSession) Data(r io.Reader) error {
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
		db.MailTypeOutbound,
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
			Msg("failed to publish mail event for transfer")
	}

	return nil
}

func (s *authSession) Logout() error {
	s.acct = nil
	return nil
}

func (s *authSession) Mail(from string, opts *smtp.MailOptions) error {
	if s.acct == nil {
		return smtp.ErrAuthRequired
	}

	from = strings.ToLower(from)
	if from != s.acct.Email() {
		return errRelayInvalidCredentials
	}

	s.bodyEncoding = opts.Body
	s.from = from
	return nil
}

func (s *authSession) Rcpt(to string, opts *smtp.RcptOptions) error {
	if s.to == nil {
		s.to = make([]string, 0, 1)
	}
	s.to = append(s.to, to)

	return nil
}

func (s *authSession) Reset() {
	s.bodyEncoding = ""
	s.from = ""
	s.to = nil
}

func NewAuthSession(c *smtp.Conn) (smtp.AuthSession, error) {
	return &authSession{
		conn: c,
	}, nil
}
