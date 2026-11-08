package db

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDomainNew(t *testing.T) {
	apex := fmt.Sprintf("%s.test", t.Name())

	t.Run("success", func(t *testing.T) {
		name := fmt.Sprintf("success.%s.%s", Rands(4), apex)
		attrs := DomainAttrs{}
		d, err := NewDomain(t.Context(), name, attrs)
		require.NoError(t, err)

		require.Len(t, d.Id(), 26)
		require.Greater(t, d.Rev(), int32(0))
		require.Equal(t, strings.ToLower(name), d.Name())

		attrs.Rev = d.Rev()
		require.Equal(t, attrs, d.Attrs())

		{
			qry := `
				select
					id,
					name,
					attrs
				from domains
				where id = $1::text;
			`

			var id string
			var name string
			var attrs DomainAttrs
			require.NoError(t, pg.QueryRow(t.Context(), qry, d.Id()).Scan(&id, &name, &attrs))

			require.Equal(t, d.Id(), id)
			require.Equal(t, d.Name(), name)
			require.Equal(t, d.Attrs(), attrs)
		}
	})

	t.Run("error: invalid domain", func(t *testing.T) {
		d, err := NewDomain(t.Context(), "invalid", DomainAttrs{})
		require.Nil(t, d)
		require.ErrorIs(t, err, ErrInvalidData)
		require.Equal(t, "invalid data", err.Error())
	})
}
