package mta

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"

	"google.golang.org/api/idtoken"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/credentials/oauth"

	"github.com/klara-to/mx/internal/pb"
)

func newMtaClient(ctx context.Context, mtaAddr string, mtaAud string) (pb.MtaClient, error) {
	var opts []grpc.DialOption
	if strings.HasPrefix(mtaAddr, "localhost:") {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else if strings.HasPrefix(mtaAddr, "https://127.0.0.1:") {
		// Used in tests, when creating mock MTAs
		opts = append(opts,
			grpc.WithTransportCredentials(
				credentials.NewTLS(&tls.Config{
					InsecureSkipVerify: true,
				}),
			),
		)

		mtaAddr = strings.TrimPrefix(mtaAddr, "https://")
	} else {
		// TODO: check MTA addr against a static set (to avoid DB tampering, email redirects and auth token hijacking)

		if mtaAud == "" {
			mtaAud = fmt.Sprintf("https://%s", mtaAddr)
		}

		ts, err := idtoken.NewTokenSource(ctx, mtaAud)
		if err != nil {
			return nil, err
		}

		opts = append(opts,
			grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{})),
			grpc.WithPerRPCCredentials(oauth.TokenSource{TokenSource: ts}),
		)
	}

	conn, err := grpc.NewClient(mtaAddr, opts...)
	if err != nil {
		return nil, err
	}

	return pb.NewMtaClient(conn), nil
}
