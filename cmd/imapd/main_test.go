//go:build test

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uatuko/go-imap"

	"go.klara.mx/internal/db"
)

func TestMain(m *testing.M) {
	// Setup
	if err := db.Setup(); err != nil {
		fmt.Printf("[error] db setup failed (%v)\n", err)
		os.Exit(1)
	}

	// Run
	code := m.Run()

	// Teardown
	db.Teardown()

	os.Exit(code)
}

type seqUid struct {
	SeqNum uint32
	Uid    uint32
}

// addTestMessage inserts a message directly into the db (bypassing storage), similar to how
// a message would be delivered by the mail pipeline.
func addTestMessage(t *testing.T, mbox *db.Mbox, uid uint32, flags ...string) {
	t.Helper()

	_, err := mbox.NewMessage(t.Context(), uid, time.Now(), db.MsgAttrs{
		Envelope: db.MsgEnvelope{Subject: fmt.Sprintf("message %d", uid)},
		Flags:    flags,
		Size:     1,
	})
	require.NoError(t, err)
}

// fetch runs ListMessages and collects the results.
func fetch(t *testing.T, m *mailbox, uid bool, set string, items ...imap.FetchItem) []*imap.Message {
	t.Helper()

	seqset, err := imap.ParseSeqSet(set)
	require.NoError(t, err)

	if len(items) == 0 {
		items = []imap.FetchItem{imap.FetchUid, imap.FetchFlags}
	}

	ch := make(chan *imap.Message)
	errCh := make(chan error, 1)
	go func() {
		errCh <- m.ListMessages(uid, seqset, items, ch)
	}()

	msgs := make([]*imap.Message, 0)
	for msg := range ch {
		msgs = append(msgs, msg)
	}
	require.NoError(t, <-errCh)

	return msgs
}

// mockStorage replaces message storage with an in-memory store for the duration of the test.
func mockStorage(t *testing.T) map[string][]byte {
	t.Helper()

	stored := map[string][]byte{}
	var mu sync.Mutex

	read, write := newMailboxMessageReader, writeMailboxMessage
	t.Cleanup(func() {
		newMailboxMessageReader, writeMailboxMessage = read, write
	})

	newMailboxMessageReader = func(_ context.Context, _, _, mboxId string, uid uint32) (io.ReadCloser, error) {
		mu.Lock()
		defer mu.Unlock()

		data, ok := stored[fmt.Sprintf("%s/%d", mboxId, uid)]
		if !ok {
			return nil, fmt.Errorf("object not found")
		}

		return io.NopCloser(bytes.NewReader(data)), nil
	}

	writeMailboxMessage = func(_ context.Context, _, _, mboxId string, uid uint32, data []byte) (int, error) {
		mu.Lock()
		defer mu.Unlock()

		stored[fmt.Sprintf("%s/%d", mboxId, uid)] = data
		return len(data), nil
	}

	return stored
}

// newTestMailbox returns the INBOX of a new test user.
func newTestMailbox(t *testing.T) (*user, *mailbox) {
	t.Helper()

	u := newTestUser(t)
	mb, err := u.GetMailbox(imap.InboxName)
	require.NoError(t, err)

	return u, mb.(*mailbox)
}

// newTestUser creates a new domain and account and returns a logged in user.
func newTestUser(t *testing.T) *user {
	t.Helper()

	b := make([]byte, 6)
	rand.Read(b)
	domain := fmt.Sprintf("imapd-%s.test", hex.EncodeToString(b))

	_, err := db.NewDomain(t.Context(), domain, db.DomainAttrs{})
	require.NoError(t, err)

	acct, err := db.NewAcct(t.Context(), domain, "user", db.AcctAttrs{})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	return &user{
		acct:   acct,
		cancel: cancel,
		ctx:    ctx,
	}
}

func readLiteral(t *testing.T, l imap.Literal) string {
	t.Helper()

	require.NotNil(t, l)
	b, err := io.ReadAll(l)
	require.NoError(t, err)

	return string(b)
}

func seqUids(msgs []*imap.Message) []seqUid {
	s := make([]seqUid, 0, len(msgs))
	for _, msg := range msgs {
		s = append(s, seqUid{SeqNum: msg.SeqNum, Uid: msg.Uid})
	}

	return s
}

func storageKey(m *mailbox, uid uint32) string {
	return fmt.Sprintf("%s/%d", m.mbox.Id(), uid)
}
