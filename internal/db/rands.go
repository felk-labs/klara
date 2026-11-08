package db

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
)

var encoding = base32.NewEncoding("0123456789abcdefghijklmnopqrstuv").WithPadding(-1)

func Rands(n int) string {
	if n < 4 {
		panic(fmt.Errorf("length too small, want at least 4 but got %d", n))
	}

	bytes := make([]byte, encoding.DecodedLen(n))
	rand.Read(bytes)

	return encoding.EncodeToString(bytes)
}
