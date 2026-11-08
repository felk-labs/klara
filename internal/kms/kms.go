package kms

import (
	"context"
	"crypto"
)

func Init(ctx context.Context, quotaProject string, keyRing string) error {
	var err error
	client, err = newGcloudKms(ctx, quotaProject, keyRing)
	if err != nil {
		return err
	}

	return nil
}

func PublicKey(ctx context.Context, name string, version string) (crypto.PublicKey, error) {
	return client.PublicKey(ctx, name, version)
}

func NewSigner(ctx context.Context, keyName string, keyVersion string) crypto.Signer {
	return client.NewSigner(ctx, keyName, keyVersion)
}
