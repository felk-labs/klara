package db

import (
	"bytes"
	"crypto/rand"

	"golang.org/x/crypto/argon2"
)

const (
	CredentialTypArgon2 CredentialTyp = "argon2"
)

type CredentialTyp string

type Credential struct {
	Id  string        `json:"id,omitempty"`
	Typ CredentialTyp `json:"typ,omitempty"`

	// Argon2
	Salt    []byte `json:"salt,omitempty"`
	Key     []byte `json:"key,omitempty"`
	Time    uint32 `json:"time,omitempty"`
	Memory  uint32 `json:"memory,omitempty"`
	Threads uint8  `json:"threads,omitempty"`
}

func (c *Credential) Verify(password string) bool {
	key := argon2.IDKey([]byte(password), c.Salt, c.Time, c.Memory, c.Threads, uint32(len(c.Key)))

	return bytes.Equal(c.Key, key)
}

func NewCredentialArgon2(password []byte) Credential {
	cred := Credential{
		Id:      uuidv7(),
		Typ:     CredentialTypArgon2,
		Salt:    make([]byte, 16),
		Time:    3,
		Memory:  64 * 1024, // 64 MB
		Threads: 4,
	}

	rand.Read(cred.Salt)
	cred.Key = argon2.IDKey(password, cred.Salt, cred.Time, cred.Memory, cred.Threads, 32)

	return cred
}
