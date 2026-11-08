package kms

import (
	"context"
	"crypto"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"

	kms "cloud.google.com/go/kms/apiv1"
	"cloud.google.com/go/kms/apiv1/kmspb"
	"github.com/rs/zerolog"
	"google.golang.org/api/option"
)

type gcloudKms struct {
	client *kms.KeyManagementClient

	// Key ring resource name
	// (e.g. projects/<project>/locations/<location>/keyRings/<name>)
	keyRing string
}

func (kms *gcloudKms) PublicKey(ctx context.Context, name string, version string) (crypto.PublicKey, error) {
	resp, err := kms.client.GetPublicKey(ctx,
		&kmspb.GetPublicKeyRequest{
			Name: kms.keyName(name, version),
		},
	)

	if err != nil {
		return nil, err
	}

	block, _ := pem.Decode([]byte(resp.GetPem()))
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}

	return pub, nil
}

func (kms *gcloudKms) NewSigner(ctx context.Context, keyName string, keyVersion string) crypto.Signer {
	return &gcloudKmsSigner{
		ctx:        ctx,
		keyName:    keyName,
		keyVersion: keyVersion,
		kms:        kms,
	}
}

func (kms *gcloudKms) keyName(name, version string) string {
	return fmt.Sprintf("%s/cryptoKeys/%s/cryptoKeyVersions/%s", kms.keyRing, name, version)
}

type gcloudKmsSigner struct {
	ctx        context.Context
	keyName    string
	keyVersion string
	kms        *gcloudKms
}

func (s *gcloudKmsSigner) Public() crypto.PublicKey {
	pub, err := PublicKey(s.ctx, s.keyName, s.keyVersion)
	if err != nil {
		log := zerolog.Ctx(s.ctx)
		log.Error().Err(err).Msg("failed to retrieve public key")

		return nil
	}

	return pub
}

func (s *gcloudKmsSigner) Sign(rand io.Reader, digest []byte, opts crypto.SignerOpts) (signature []byte, err error) {
	req := &kmspb.AsymmetricSignRequest{
		Name: s.kms.keyName(s.keyName, s.keyVersion),
	}

	switch opts.HashFunc() {
	case 0:
		// ed25519
		req.Data = digest
	case crypto.SHA256:
		// rsa-sha256
		req.Digest = &kmspb.Digest{
			Digest: &kmspb.Digest_Sha256{
				Sha256: digest,
			},
		}

	default:
		panic("unknown signature hash")
	}

	resp, err := s.kms.client.AsymmetricSign(s.ctx, req)
	if err != nil {
		return nil, err
	}

	return resp.GetSignature(), nil
}

func newGcloudKms(ctx context.Context, quotaProject string, keyRing string) (Client, error) {
	client, err := kms.NewKeyManagementClient(ctx, option.WithQuotaProject(quotaProject))
	if err != nil {
		return nil, err
	}

	return &gcloudKms{
		client:  client,
		keyRing: keyRing,
	}, nil
}
