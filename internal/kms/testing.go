//go:build test

package kms

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	"sync"
)

type mockKms struct {
	keys map[string]crypto.PrivateKey

	sync.Mutex
}

func (kms *mockKms) PublicKey(ctx context.Context, name string, version string) (crypto.PublicKey, error) {
	key, err := kms.key(name, version)
	if err != nil {
		return nil, err
	}

	return key.(interface{ Public() crypto.PublicKey }).Public(), nil
}

func (kms *mockKms) NewSigner(ctx context.Context, keyName string, keyVersion string) crypto.Signer {
	k, err := kms.key(keyName, keyVersion)
	if err != nil {
		panic(err)
	}

	return &mockKmsSigner{
		key: k,
	}
}

func (kms *mockKms) key(name string, version string) (crypto.PrivateKey, error) {
	kms.Lock()
	defer kms.Unlock()

	keyName := fmt.Sprintf("%s/%s", name, version)
	key, ok := kms.keys[keyName]
	if !ok {
		_, k, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, err
		}

		kms.keys[keyName] = k
		key = k
	}

	return key, nil
}

type mockKmsSigner struct {
	key crypto.PrivateKey
}

func (s *mockKmsSigner) Public() crypto.PublicKey {
	return s.key.(interface{ Public() crypto.PublicKey }).Public()
}

func (s *mockKmsSigner) Sign(rand io.Reader, digest []byte, opts crypto.SignerOpts) (signature []byte, err error) {
	k := s.key.(ed25519.PrivateKey)
	return k.Sign(rand, digest, opts)
}

func Setup() error {
	client = &mockKms{
		keys: map[string]crypto.PrivateKey{},
	}

	return nil
}

func Teardown() {
	client = nil
}
