package db

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAcctRetrieveByEmail(t *testing.T) {
	domain := fmt.Sprintf("%s.%s.test", strings.ToLower(Rands(8)), strings.ToLower(t.Name()))
	_, err := NewDomain(t.Context(), domain, DomainAttrs{})
	require.NoError(t, err)

	acct, err := NewAcct(t.Context(), domain, "Jane.Doe", AcctAttrs{})
	require.NoError(t, err)
	require.Equal(t, "jane.doe", acct.Name())

	t.Run("success", func(t *testing.T) {
		a, err := RetrieveAcctByEmail(t.Context(), "jane.doe@"+domain)
		require.NoError(t, err)
		require.Equal(t, acct.Id(), a.Id())
	})

	t.Run("success: case insensitive", func(t *testing.T) {
		for _, email := range []string{
			"Jane.Doe@" + domain,
			"JANE.DOE@" + strings.ToUpper(domain),
		} {
			a, err := RetrieveAcctByEmail(t.Context(), email)
			require.NoError(t, err, email)
			require.Equal(t, acct.Id(), a.Id(), email)
		}
	})

	t.Run("error: not found", func(t *testing.T) {
		_, err := RetrieveAcctByEmail(t.Context(), "john.doe@"+domain)
		require.ErrorIs(t, err, ErrNotFound)
	})
}
