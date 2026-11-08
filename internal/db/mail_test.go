package db

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMailNew(t *testing.T) {
	ts := time.UnixMicro(time.Now().UnixMicro()).UTC()
	data := []byte("data")
	attrs := MailAttrs{}

	mail, err := NewMail(t.Context(), ts, mailTypeTest, data, attrs)
	require.NoError(t, err)

	require.Len(t, mail.Id(), 26)
	require.Greater(t, mail.Rev(), int32(0))
	require.Equal(t, ts, mail.Ts())
	require.Equal(t, mailTypeTest, mail.Typ())
	require.Equal(t, MailStatusPending, mail.Status())
	require.Equal(t, data, mail.Data())

	attrs.Rev = mail.Rev()
	require.Equal(t, attrs, mail.Attrs())

	{
		qry := `
			select
				id,
				ts,
				typ,
				status,
				data,
				attrs,
				(attrs -> '_rev')::integer
			from mail
			where id = $1::text;
		`

		var id string
		var ts time.Time
		var typ MailType
		var status MailStatus
		var data []byte
		var attrs MailAttrs
		var rev int32
		require.NoError(t, pg.QueryRow(t.Context(), qry, mail.Id()).Scan(&id, &ts, &typ, &status, &data, &attrs, &rev))

		require.Equal(t, mail.Id(), id)
		require.Equal(t, mail.Ts(), ts)
		require.Equal(t, mail.Typ(), typ)
		require.Equal(t, mail.Status(), status)
		require.Equal(t, mail.Data(), data)
		require.Equal(t, mail.Attrs(), attrs)
		require.Equal(t, mail.Rev(), rev)
	}
}

func TestMailSetStatus(t *testing.T) {
	mail, err := NewMail(t.Context(), time.Now(), mailTypeTest, []byte("data"), MailAttrs{})
	require.NoError(t, err)

	require.Equal(t, MailStatusPending, mail.Status())
	require.NoError(t, mail.SetStatus(t.Context(), MailStatusProcessing))
	require.Equal(t, MailStatusProcessing, mail.Status())

	t.Run("error: revision mismatch", func(t *testing.T) {
		rev := mail.Rev() // backup
		mail.attrs.Rev = rev - 1
		err = mail.SetStatus(t.Context(), MailStatusProcessing)

		require.ErrorIs(t, err, ErrConflict)
		require.Equal(t, "data conflict", err.Error())

		mail.attrs.Rev = rev // restore
	})
}
