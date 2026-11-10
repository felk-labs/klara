//go:build test

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uatuko/go-imap"

	"go.klara.mx/internal/db"
)

func TestUserGetMailbox(t *testing.T) {
	// Messages delivered to a mailbox after login (e.g. by the mail pipeline) must be visible
	// to subsequent SELECT / STATUS commands in the same session.
	t.Run("status reflects new messages", func(t *testing.T) {
		u := newTestUser(t)

		mb, err := u.GetMailbox(imap.InboxName)
		require.NoError(t, err)

		s, err := mb.Status([]imap.StatusItem{imap.StatusMessages, imap.StatusUidNext})
		require.NoError(t, err)
		require.Equal(t, uint32(0), s.Messages)
		require.Equal(t, uint32(1), s.UidNext)

		// Deliver a message out-of-band
		mbox, err := db.RetrieveMboxByAcctId(t.Context(), u.AcctId(), db.MboxNameInbox)
		require.NoError(t, err)
		uid, err := mbox.IncrUidNext(t.Context())
		require.NoError(t, err)
		addTestMessage(t, mbox, uid)

		mb, err = u.GetMailbox(imap.InboxName)
		require.NoError(t, err)

		s, err = mb.Status([]imap.StatusItem{imap.StatusMessages, imap.StatusUidNext})
		require.NoError(t, err)
		require.Equal(t, uint32(1), s.Messages)
		require.Equal(t, uint32(2), s.UidNext)
	})
}

func TestUserListMailboxes(t *testing.T) {
	// `LIST "" INBOX` must return the INBOX.
	// Ref: https://www.rfc-editor.org/rfc/rfc9051#section-5.1
	t.Run("inbox", func(t *testing.T) {
		u := newTestUser(t)

		mbs, err := u.ListMailboxes(false)
		require.NoError(t, err)

		var matched []string
		for _, mb := range mbs {
			info, err := mb.Info()
			require.NoError(t, err)

			if info.Match("", imap.InboxName) {
				matched = append(matched, info.Name)
			}
		}

		require.Equal(t, []string{imap.InboxName}, matched)
	})
}
