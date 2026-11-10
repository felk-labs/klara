package main

import (
	"context"
	"fmt"
	"os"
	"time"

	imapbe "github.com/uatuko/go-imap/backend"

	"go.klara.mx/internal/db"
)

type user struct {
	acct   *db.Acct
	mboxes map[string]*mailbox

	ctx    context.Context
	cancel context.CancelFunc
}

func (u *user) AcctId() string {
	return u.acct.Id()
}

func (u *user) Username() string {
	return u.acct.Email()
}

func (u *user) ListMailboxes(subscribed bool) ([]imapbe.Mailbox, error) {
	err := u.refreshMailboxes()
	if err != nil {
		return nil, errUnknown
	}

	mbs := make([]imapbe.Mailbox, 0, len(u.mboxes))
	for _, m := range u.mboxes {
		mbs = append(mbs, m)
	}

	return mbs, nil
}

func (u *user) GetMailbox(name string) (imapbe.Mailbox, error) {
	err := u.refreshMailboxes()
	if err != nil {
		return nil, errUnknown
	}

	m, ok := u.mboxes[cannonicalMailboxName(name)]
	if !ok {
		return nil, errMailboxNotFound
	}

	return m, nil
}

func (u *user) CreateMailbox(name string) error {
	return errNotImplemented
}

func (u *user) DeleteMailbox(name string) error {
	return errNotImplemented
}

func (u *user) RenameMailbox(existingName, newName string) error {
	return errNotImplemented
}

func (u *user) Logout() error {
	u.cancel()
	return nil
}

// refreshMailboxes retrieves mailboxes for the user and caches in memory.
// It will attempt to create any missing default / special-use mailboxes for the user.
//
// WARNING: this function is not concurrent safe, but `uatuko/go-imap` currently doesn't
// support pipelining so it is assumed there won't be concurrent calls.
func (u *user) refreshMailboxes() error {
	if u.mboxes != nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(u.ctx, 15*time.Second)
	defer cancel()

	mboxes, err := db.ListMboxesByAcctId(ctx, u.AcctId())
	if err != nil {
		return err
	}

	have := map[string]bool{}
	u.mboxes = make(map[string]*mailbox)
	for i := range mboxes {
		m := newMailbox(u.ctx, u.acct, &mboxes[i])
		u.mboxes[m.Name()] = m
		have[m.mbox.Name()] = true
	}

	// Create any missing mailboxes
	want := []string{db.MboxNameArchive, db.MboxNameInbox, db.MboxNameJunk, db.MboxNameSent, db.MboxNameTrash}
	for _, name := range want {
		_, ok := have[name]
		if ok {
			continue
		}

		mbox, err := db.NewMbox(ctx, u.AcctId(), name, db.MboxAttrs{})
		if err != nil {
			fmt.Fprintf(os.Stderr, "[warn] failed to create mailbox, err: %v\n", err)
			continue
		}

		m := newMailbox(u.ctx, u.acct, mbox)
		u.mboxes[m.Name()] = m
	}

	return nil
}
