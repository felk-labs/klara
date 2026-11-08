package mta

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/klara-to/mx/internal/db"
	"github.com/klara-to/mx/internal/kms"
	"github.com/klara-to/mx/internal/pb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uatuko/go-message"
	"github.com/uatuko/go-msgauth/dkim"
)

func TestTransfer(t *testing.T) {
	// Mock server
	var mtaReq *pb.MtaTransferRequest
	grpcServer := NewMockMtaServer(func(ctx context.Context, req *pb.MtaTransferRequest) (*pb.MtaTransferResponse, error) {
		mtaReq = req
		return &pb.MtaTransferResponse{}, nil
	})

	ts := httptest.NewUnstartedServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ProtoMajor == 2 && strings.HasPrefix(
				r.Header.Get("Content-Type"), "application/grpc") {
				grpcServer.ServeHTTP(w, r)
				return
			}

			w.WriteHeader(http.StatusNotImplemented)
		}),
	)
	ts.EnableHTTP2 = true
	ts.StartTLS()
	defer ts.Close()

	// Sending domain
	domainName := fmt.Sprintf("%s.%s.test", db.Rands(4), strings.ToLower(t.Name()))
	d, err := db.NewDomain(
		t.Context(),
		domainName,
		db.DomainAttrs{
			DomainKeyName:     strings.ReplaceAll(domainName, ".", "-"),
			DomainKeySelector: "test",
			DomainKeyVersion:  "1",
			MtaAddr:           ts.URL,
			MtaHostname:       fmt.Sprintf("mail.%s", domainName),
		},
	)
	require.NoError(t, err)

	// Outbound mail
	from := fmt.Sprintf("from@%s", domainName)
	to := fmt.Sprintf("to@%s", domainName)
	data := fmt.Appendf(nil, `Date: Sun, 5 Apr 2026 18:42:56 +0100
MIME-Version: 1.0
User-Agent: Mozilla Thunderbird
Content-Language: en-GB
From: From <%s>
To: %s
Subject: Test - plain text
Content-Type: text/plain; charset=UTF-8; format=flowed
Content-Transfer-Encoding: 7bit

This should be a plain text message.
`, from, to)

	m, err := db.NewMail(
		t.Context(),
		time.Now(),
		db.MailTypeOutbound,
		data,
		db.MailAttrs{
			Envelope: db.MailEnvelope{
				From: from,
				To:   []string{to},
			},
		},
	)
	require.NoError(t, err)

	// Transfer
	require.NoError(t, Transfer(t.Context(), m.Id()))

	// Assertions
	require.NotNil(t, mtaReq)
	assert.Equal(t, m.ReturnPath(), mtaReq.GetReturnPath())
	assert.ElementsMatch(t, m.Recipients(), mtaReq.GetRecipients())

	me, err := message.Read(bytes.NewReader(mtaReq.GetData()))
	require.NoError(t, err)

	assert.Equal(t,
		map[string][]string{
			// From outbound mail
			"Date":                      {"Sun, 5 Apr 2026 18:42:56 +0100"},
			"Mime-Version":              {"1.0"},
			"User-Agent":                {"Mozilla Thunderbird"},
			"Content-Language":          {"en-GB"},
			"From":                      {fmt.Sprintf("From <%s>", from)},
			"To":                        {to},
			"Subject":                   {"Test - plain text"},
			"Content-Type":              {"text/plain; charset=UTF-8; format=flowed"},
			"Content-Transfer-Encoding": {"7bit"},

			// Added by mta
			"Dkim-Signature": me.Header.Values("Dkim-Signature"),
			"Message-Id":     me.Header.Values("Message-Id"),
		},
		me.Header.Map(),
	)
	assert.True(t, strings.HasSuffix(
		me.Header.Get("Message-Id"),
		// e.g. <DHM309JN4TAG.2TG3IIRSA9X90@mail.j11g.testtransfer.test>
		fmt.Sprintf("@%s>", d.MtaHostname()),
	))

	// Verify dkim signature
	verfOpts := dkim.VerifyOptions{
		LookupTXT: func(name string) ([]string, error) {
			if name != fmt.Sprintf("%s._domainkey.%s", d.DomainKeySelector(), domainName) {
				return nil, &net.DNSError{
					Err:        "not found",
					IsNotFound: true,
				}
			}

			k, err := kms.PublicKey(t.Context(), d.DomainKeyName(), d.DomainKeyVersion())
			if err != nil {
				return nil, err
			}

			return []string{
				fmt.Sprintf("v=DKIM1; k=ed25519; p=%s",
					base64.StdEncoding.EncodeToString(k.(ed25519.PublicKey)),
				),
			}, nil
		},
	}

	verfs, err := dkim.VerifyWithOptions(bytes.NewReader(mtaReq.GetData()), &verfOpts)
	require.NoError(t, err)
	require.Len(t, verfs, 1)

	verf := verfs[0]
	require.NoError(t, verf.Err)
	require.Equal(t, domainName, verf.Domain)

	assert.ElementsMatch(t,
		[]string{
			"Message-Id",
			"MIME-Version",
			"Date",
			"User-Agent",
			"Content-Language",
			"From",
			"To",
			"Subject",
			"Content-Type",
			"Content-Transfer-Encoding",
		},
		verf.HeaderKeys,
	)
}
