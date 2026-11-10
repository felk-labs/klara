package srv

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uatuko/go-message"

	"go.klara.mx/internal/db"
	"go.klara.mx/internal/events"
	"go.klara.mx/pb"
)

func TestMailSend(t *testing.T) {
	domainName := fmt.Sprintf("%s.%s.test", db.Rands(4), strings.ToLower(t.Name()))
	_, err := db.NewDomain(
		t.Context(),
		domainName,
		db.DomainAttrs{
			DomainKeyName:     strings.ReplaceAll(domainName, ".", "-"),
			DomainKeySelector: "test",
			DomainKeyVersion:  "1",
			MtaHostname:       fmt.Sprintf("mail.%s", domainName),
		},
	)
	require.NoError(t, err)

	req := &pb.MailSendRequest{
		From: &pb.Address{
			Email: fmt.Sprintf("from@%s", domainName),
			Name:  "From",
		},
		To: []*pb.Address{
			{Email: fmt.Sprintf("to-1@%s", domainName)},
		},
		Cc: []*pb.Address{
			{
				Email: fmt.Sprintf("cc-1@%s", domainName),
				Name:  "One Cc",
			},
			{Email: fmt.Sprintf("cc-2@%s", domainName)},
		},
		Bcc: []*pb.Address{
			{Email: fmt.Sprintf("bcc-1@%s", domainName)},
		},
		Subject: fmt.Sprintf("[test] %s", t.Name()),
		Content: []*pb.Message{
			{Data: "<h1>Hello World!</h1><p>This is an HTML part.</p>", Type: "text/html"},
			{Data: "👋\r\n\r\nThis is a text part.", Type: "text/plain"},
		},
	}

	ch := events.NewListener()
	srv := &mail{}
	resp, err := srv.Send(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	for range len(req.Bcc) + 1 {
		ev := <-ch
		pbMail := ev.Unwrap().GetMail()
		require.NotNil(t, pbMail)

		mail, err := db.RetrieveMail(t.Context(), pbMail.GetId())
		require.NoError(t, err)

		attrs := mail.Attrs()
		require.Equal(t, req.From.GetEmail(), attrs.Envelope.From)

		to := make([]string, 0)
		if len(attrs.Envelope.To) == 1 {
			// Bcc
			to = append(to, req.Bcc[0].GetEmail())
		} else {
			for _, pbAddr := range req.To {
				to = append(to, pbAddr.GetEmail())
			}

			for _, pbAddr := range req.Cc {
				to = append(to, pbAddr.GetEmail())
			}
		}
		require.Equal(t, to, attrs.Envelope.To)

		msg, err := message.Read(bytes.NewReader(mail.Data()))
		require.NoError(t, err)

		expectedHdr := map[string][]string{
			hdrMimeVersionKey: {"1.0"},
			"Date":            msg.Header.Values("Date"),
			"Subject":         {req.Subject},
			hdrFromKey:        {fmt.Sprintf("\"%s\" <%s>", req.From.Name, req.From.Email)},
			hdrToKey:          {strings.Join(mapList(req.To, mapPbAddrToString), ", ")},
			hdrCcKey:          {strings.Join(mapList(req.Cc, mapPbAddrToString), ", ")},
		}
		if len(attrs.Envelope.To) == 1 {
			// Bcc
			expectedHdr[hdrBccKey] = mapList(req.Bcc, mapPbAddrToString)
		}
		expectedHdr[hdrContentTypeKey] = msg.Header.Values(hdrContentTypeKey)
		require.Equal(t, expectedHdr, msg.Header.Map())

		mr := msg.MultipartReader()
		require.NotNil(t, mr)
		for i := range len(req.Content) {
			part, err := mr.NextPart()
			require.NoError(t, err)

			pbMsg := req.Content[i]
			typ, params, err := part.Header.ContentType()
			require.NoError(t, err)
			require.Equal(t, typ, pbMsg.Type)
			require.Empty(t, params)

			assert.Equal(t, "inline", part.Header.Get("Content-Disposition"))
			assert.Equal(t, "quoted-printable", part.Header.Get("Content-Transfer-Encoding"))

			var buf bytes.Buffer
			io.Copy(&buf, part.Body)

			require.Equal(t, pbMsg.Data, buf.String())
		}
	}

	select {
	case <-ch:
		t.Fatal("More events published than expected")
	default:
		// No more events, as expected
	}
}

func mapPbAddrToString(pbAddr *pb.Address) string {
	s := fmt.Sprintf("<%s>", pbAddr.GetEmail())
	if pbAddr.GetName() != "" {
		s = fmt.Sprintf("\"%s\" %s", pbAddr.GetName(), s)
	}

	return s
}
