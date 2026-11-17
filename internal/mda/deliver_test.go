package mda

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBaseAddress(t *testing.T) {
	tests := []struct {
		name string
		rcpt string
		want string
	}{
		{name: "no subaddress", rcpt: "user@example.com", want: "user@example.com"},
		{name: "subaddress", rcpt: "user+xxxx@example.com", want: "user@example.com"},
		{name: "empty detail", rcpt: "user+@example.com", want: "user@example.com"},
		{name: "multiple separators", rcpt: "user+a+b@example.com", want: "user@example.com"},
		{name: "dotted name", rcpt: "jane.doe+news@example.com", want: "jane.doe@example.com"},
		{name: "empty base", rcpt: "+xxxx@example.com", want: "+xxxx@example.com"},
		{name: "no domain", rcpt: "user+xxxx", want: "user+xxxx"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, baseAddress(tt.rcpt))
		})
	}
}
