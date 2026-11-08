package kms

import (
	"context"
	"crypto"
)

var client Client

type Client interface {
	PublicKey(ctx context.Context, name string, version string) (crypto.PublicKey, error)
	NewSigner(ctx context.Context, keyName string, keyVersion string) crypto.Signer
}
