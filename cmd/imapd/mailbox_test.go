//go:build test

package main

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uatuko/go-imap"
	imapbe "github.com/uatuko/go-imap/backend"

	"github.com/klara-to/mx/internal/db"
)

func TestCannonicalMailboxName(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"INBOX", "INBOX"},
		{"inbox", "INBOX"},
		{"Inbox", "INBOX"},
		{"iNbOx", "INBOX"},
		{"sent", "Sent"},
		{"SENT", "Sent"},
		{"Trash", "Trash"},
		{"Custom", "Custom"},
		{"custom", "custom"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, cannonicalMailboxName(tt.name))
		})
	}
}

func TestMailboxCreateMessage(t *testing.T) {
	stored := mockStorage(t)

	// INTERNALDATE must be the date given to APPEND, and the ENVELOPE date must be the
	// message Date header.
	// Ref: https://www.rfc-editor.org/rfc/rfc9051#section-6.3.12
	// Ref: https://www.rfc-editor.org/rfc/rfc9051#section-7.5.2
	t.Run("dates", func(t *testing.T) {
		_, m := newTestMailbox(t)

		internalDate := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
		data := "Date: Mon, 7 Feb 1994 21:52:25 -0800\r\n" +
			"From: Fred Foobar <foobar@example.com>\r\n" +
			"Subject: test\r\n" +
			"\r\n" +
			"Hello\r\n"

		require.NoError(t, m.CreateMessage([]string{imap.SeenFlag}, internalDate, bytes.NewBufferString(data)))
		require.Equal(t, []byte(data), stored[storageKey(m, 1)])

		msgs := fetch(t, m, false, "1", imap.FetchInternalDate, imap.FetchEnvelope, imap.FetchFlags, imap.FetchRFC822Size)
		require.Len(t, msgs, 1)
		require.True(t, internalDate.Equal(msgs[0].InternalDate), "internal date: %v", msgs[0].InternalDate)

		envDate := time.Date(1994, 2, 7, 21, 52, 25, 0, time.FixedZone("", -8*60*60))
		require.True(t, envDate.Equal(msgs[0].Envelope.Date), "envelope date: %v", msgs[0].Envelope.Date)
		require.Equal(t, "test", msgs[0].Envelope.Subject)
		require.Equal(t, []string{imap.SeenFlag}, msgs[0].Flags)
		require.Equal(t, uint32(len(data)), msgs[0].Size)
	})

	// Without a date argument, the internal date defaults to the current time.
	// Without a Date header, the envelope date is NIL.
	t.Run("no dates", func(t *testing.T) {
		_, m := newTestMailbox(t)

		data := "Subject: test\r\n" +
			"\r\n" +
			"Hello\r\n"

		before := time.Now().Truncate(time.Microsecond)
		require.NoError(t, m.CreateMessage(nil, time.Time{}, bytes.NewBufferString(data)))
		after := time.Now()

		msgs := fetch(t, m, false, "1", imap.FetchInternalDate, imap.FetchEnvelope)
		require.Len(t, msgs, 1)
		require.False(t, msgs[0].InternalDate.Before(before), "internal date: %v", msgs[0].InternalDate)
		require.False(t, msgs[0].InternalDate.After(after), "internal date: %v", msgs[0].InternalDate)
		require.True(t, msgs[0].Envelope.Date.IsZero(), "envelope date: %v", msgs[0].Envelope.Date)
	})

	t.Run("too big", func(t *testing.T) {
		_, m := newTestMailbox(t)

		data := bytes.Repeat([]byte("a"), maxMessageSize+1)
		err := m.CreateMessage(nil, time.Time{}, bytes.NewBuffer(data))
		require.True(t, err == imapbe.ErrTooBig, "err: %v", err)

		msgs := fetch(t, m, false, "1:*")
		require.Empty(t, msgs)
	})
}

func TestMailboxListMessages(t *testing.T) {
	t.Run("seq range", func(t *testing.T) {
		_, m := newTestMailbox(t)
		for uid := uint32(1); uid <= 3; uid++ {
			addTestMessage(t, m.mbox, uid)
		}

		msgs := fetch(t, m, false, "2:3")
		require.Equal(t, []seqUid{{2, 2}, {3, 3}}, seqUids(msgs))
	})

	// "*" represents the last message in the mailbox.
	// Ref: https://www.rfc-editor.org/rfc/rfc9051#section-9 (seq-number)
	t.Run("seq *", func(t *testing.T) {
		_, m := newTestMailbox(t)
		for uid := uint32(1); uid <= 3; uid++ {
			addTestMessage(t, m.mbox, uid)
		}

		msgs := fetch(t, m, false, "*")
		require.Equal(t, []seqUid{{3, 3}}, seqUids(msgs))
	})

	t.Run("uid *", func(t *testing.T) {
		_, m := newTestMailbox(t)
		for uid := uint32(1); uid <= 3; uid++ {
			addTestMessage(t, m.mbox, uid)
		}

		msgs := fetch(t, m, true, "*")
		require.Equal(t, []seqUid{{3, 3}}, seqUids(msgs))
	})

	// "a UID range of 559:* always includes the UID of the last message in the mailbox, even
	// if 559 is higher than any assigned UID value."
	// Ref: https://www.rfc-editor.org/rfc/rfc9051#section-6.4.9
	t.Run("uid n:* beyond last uid", func(t *testing.T) {
		_, m := newTestMailbox(t)
		for uid := uint32(1); uid <= 3; uid++ {
			addTestMessage(t, m.mbox, uid)
		}

		msgs := fetch(t, m, true, "10:*")
		require.Equal(t, []seqUid{{3, 3}}, seqUids(msgs))
	})

	t.Run("uid 1:* with large uids", func(t *testing.T) {
		_, m := newTestMailbox(t)
		addTestMessage(t, m.mbox, 1)
		addTestMessage(t, m.mbox, 2000)

		msgs := fetch(t, m, true, "1:*")
		require.Equal(t, []seqUid{{1, 1}, {2, 2000}}, seqUids(msgs))
	})

	// Sequence numbers must be derived from the message position in the mailbox, not from
	// the requested UIDs.
	t.Run("uid seq numbers with uid gaps", func(t *testing.T) {
		_, m := newTestMailbox(t)
		addTestMessage(t, m.mbox, 1)
		addTestMessage(t, m.mbox, 5)
		addTestMessage(t, m.mbox, 9)

		msgs := fetch(t, m, true, "5:9")
		require.Equal(t, []seqUid{{2, 5}, {3, 9}}, seqUids(msgs))

		msgs = fetch(t, m, true, "2:9")
		require.Equal(t, []seqUid{{2, 5}, {3, 9}}, seqUids(msgs))
	})

	t.Run("seq 1:* more than results limit", func(t *testing.T) {
		_, m := newTestMailbox(t)
		n := uint32(db.ResultsLimit + 1)
		for uid := uint32(1); uid <= n; uid++ {
			addTestMessage(t, m.mbox, uid)
		}

		msgs := fetch(t, m, false, "1:*", imap.FetchUid)
		require.Equal(t, int(n), len(msgs))
		require.Equal(t, n, msgs[len(msgs)-1].SeqNum)
		require.Equal(t, n, msgs[len(msgs)-1].Uid)

		msgs = fetch(t, m, true, "1:*", imap.FetchUid)
		require.Equal(t, int(n), len(msgs))
		require.Equal(t, n, msgs[len(msgs)-1].SeqNum)
		require.Equal(t, n, msgs[len(msgs)-1].Uid)
	})

	t.Run("empty mailbox", func(t *testing.T) {
		_, m := newTestMailbox(t)

		require.Empty(t, fetch(t, m, false, "*"))
		require.Empty(t, fetch(t, m, false, "1:*"))
		require.Empty(t, fetch(t, m, true, "*"))
		require.Empty(t, fetch(t, m, true, "1:*"))
	})

	// "*" resolves to a value already in the set, messages must only be returned once.
	t.Run("overlapping *", func(t *testing.T) {
		_, m := newTestMailbox(t)
		for uid := uint32(1); uid <= 3; uid++ {
			addTestMessage(t, m.mbox, uid)
		}

		msgs := fetch(t, m, false, "2:3,*")
		require.Equal(t, []seqUid{{2, 2}, {3, 3}}, seqUids(msgs))

		msgs = fetch(t, m, true, "2:3,*")
		require.Equal(t, []seqUid{{2, 2}, {3, 3}}, seqUids(msgs))
	})
}

func TestMailboxListMessagesSeen(t *testing.T) {
	mockStorage(t)

	data := "Subject: test\r\n" +
		"\r\n" +
		"Hello\r\n"

	tests := []struct {
		item imap.FetchItem
		seen bool
	}{
		{"BODY[]", true},
		{"BODY[HEADER]", true},
		{"BODY[TEXT]", true},
		{"BODY[]<0.5>", true},

		// IMAP4rev1 items (deprecated in IMAP4rev2), equivalent to BODY[], BODY[TEXT] and
		// BODY.PEEK[HEADER].
		// Ref: https://www.rfc-editor.org/rfc/rfc3501#section-6.4.5
		// Ref: https://www.rfc-editor.org/rfc/rfc9051#appendix-E
		{"RFC822", true},
		{"RFC822.TEXT", true},
		{"BODY.PEEK[]", false},
		{"BODY.PEEK[HEADER]", false},
		{"RFC822.HEADER", false},
		{imap.FetchBodyStructure, false},
		{imap.FetchEnvelope, false},
	}

	// Fetching a body section (unless using BODY.PEEK) implicitly sets the \Seen flag.
	// Ref: https://www.rfc-editor.org/rfc/rfc9051#section-6.4.5
	for _, tt := range tests {
		t.Run(string(tt.item), func(t *testing.T) {
			_, m := newTestMailbox(t)
			require.NoError(t, m.CreateMessage([]string{imap.FlaggedFlag}, time.Time{}, bytes.NewBufferString(data)))

			msgs := fetch(t, m, false, "1", tt.item)
			require.Len(t, msgs, 1)

			want := []string{imap.FlaggedFlag}
			if tt.seen {
				want = append(want, imap.SeenFlag)

				// Updated flags must be included in the response
				require.Contains(t, msgs[0].Items, imap.FetchItem(imap.FetchFlags))
				require.Equal(t, want, msgs[0].Flags)
			} else {
				require.NotContains(t, msgs[0].Items, imap.FetchItem(imap.FetchFlags))
			}

			msgs = fetch(t, m, false, "1", imap.FetchFlags)
			require.Len(t, msgs, 1)
			require.Equal(t, want, msgs[0].Flags)
		})
	}

	t.Run("already seen", func(t *testing.T) {
		_, m := newTestMailbox(t)
		require.NoError(t, m.CreateMessage([]string{imap.SeenFlag}, time.Time{}, bytes.NewBufferString(data)))

		msgs := fetch(t, m, false, "1", "BODY[]", imap.FetchFlags)
		require.Len(t, msgs, 1)
		require.Equal(t, []string{imap.SeenFlag}, msgs[0].Flags)

		section, _ := imap.ParseBodySectionName("BODY[]")
		require.Equal(t, data, readLiteral(t, msgs[0].GetBody(section)))
	})
}

func TestMailboxStatus(t *testing.T) {
	t.Run("messages", func(t *testing.T) {
		_, m := newTestMailbox(t)
		addTestMessage(t, m.mbox, 1)
		addTestMessage(t, m.mbox, 2)

		s, err := m.Status([]imap.StatusItem{imap.StatusMessages})
		require.NoError(t, err)
		require.Equal(t, uint32(2), s.Messages)
	})

	// UNSEEN: the number of messages which do not have the \Seen flag set.
	// Ref: https://www.rfc-editor.org/rfc/rfc9051#section-6.3.11
	t.Run("unseen", func(t *testing.T) {
		_, m := newTestMailbox(t)
		addTestMessage(t, m.mbox, 1, imap.SeenFlag)
		addTestMessage(t, m.mbox, 2)
		addTestMessage(t, m.mbox, 3, imap.FlaggedFlag)

		s, err := m.Status([]imap.StatusItem{imap.StatusUnseen})
		require.NoError(t, err)
		require.Equal(t, uint32(2), s.Unseen)

		// SELECT: first unseen message
		require.Equal(t, uint32(2), s.UnseenSeqNum)
	})

	t.Run("all seen", func(t *testing.T) {
		_, m := newTestMailbox(t)
		addTestMessage(t, m.mbox, 1, imap.SeenFlag)

		s, err := m.Status([]imap.StatusItem{imap.StatusMessages, imap.StatusUnseen})
		require.NoError(t, err)
		require.Equal(t, uint32(1), s.Messages)
		require.Equal(t, uint32(0), s.Unseen)
		require.Equal(t, uint32(0), s.UnseenSeqNum)
	})

	t.Run("reflects flag changes", func(t *testing.T) {
		_, m := newTestMailbox(t)
		addTestMessage(t, m.mbox, 1)

		s, err := m.Status([]imap.StatusItem{imap.StatusUnseen})
		require.NoError(t, err)
		require.Equal(t, uint32(1), s.Unseen)

		seqset, _ := imap.ParseSeqSet("1")
		require.NoError(t, m.UpdateMessagesFlags(false, seqset, imap.AddFlags, []string{imap.SeenFlag}))

		s, err = m.Status([]imap.StatusItem{imap.StatusUnseen})
		require.NoError(t, err)
		require.Equal(t, uint32(0), s.Unseen)
	})
}

func TestMailboxUpdateMessagesFlags(t *testing.T) {
	t.Run("seq range", func(t *testing.T) {
		_, m := newTestMailbox(t)
		for uid := uint32(1); uid <= 3; uid++ {
			addTestMessage(t, m.mbox, uid)
		}

		seqset, _ := imap.ParseSeqSet("2")
		require.NoError(t, m.UpdateMessagesFlags(false, seqset, imap.AddFlags, []string{imap.FlaggedFlag}))

		msgs := fetch(t, m, false, "1:3")
		require.Len(t, msgs, 3)
		require.Empty(t, msgs[0].Flags)
		require.Equal(t, []string{imap.FlaggedFlag}, msgs[1].Flags)
		require.Empty(t, msgs[2].Flags)
	})

	// `STORE * +FLAGS (\Deleted)` must only affect the last message.
	t.Run("seq *", func(t *testing.T) {
		_, m := newTestMailbox(t)
		for uid := uint32(1); uid <= 3; uid++ {
			addTestMessage(t, m.mbox, uid)
		}

		seqset, _ := imap.ParseSeqSet("*")
		require.NoError(t, m.UpdateMessagesFlags(false, seqset, imap.AddFlags, []string{imap.DeletedFlag}))

		msgs := fetch(t, m, false, "1:3")
		require.Len(t, msgs, 3)
		require.Empty(t, msgs[0].Flags)
		require.Empty(t, msgs[1].Flags)
		require.Equal(t, []string{imap.DeletedFlag}, msgs[2].Flags)
	})

	t.Run("uid *", func(t *testing.T) {
		_, m := newTestMailbox(t)
		for uid := uint32(1); uid <= 3; uid++ {
			addTestMessage(t, m.mbox, uid)
		}

		seqset, _ := imap.ParseSeqSet("*")
		require.NoError(t, m.UpdateMessagesFlags(true, seqset, imap.AddFlags, []string{imap.DeletedFlag}))

		msgs := fetch(t, m, false, "1:3")
		require.Len(t, msgs, 3)
		require.Empty(t, msgs[0].Flags)
		require.Empty(t, msgs[1].Flags)
		require.Equal(t, []string{imap.DeletedFlag}, msgs[2].Flags)
	})
}
