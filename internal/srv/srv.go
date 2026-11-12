package srv

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	tknz "go.tknz.dev/pb"

	"go.klara.mx/pb"
)

const (
	authSchemeBearer  = "Bearer"
	bearerTokenPrefix = authSchemeBearer + " "

	envTknzAppIdKey = "TKNZ_APP_ID"

	metaAuthorizationKey = "authorization"
)

type CtxIdnKey struct{}
type CtxSpaceIdKey struct{}

func New() http.Handler {
	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			UnaryLoggerInterceptor,
			UnaryErrorInterceptor,
		),
	)
	pb.RegisterAccountsServer(grpcServer, &accounts{})
	pb.RegisterDomainsServer(grpcServer, &domains{})
	pb.RegisterMailServer(grpcServer, &mail{})
	pb.RegisterMailboxesServer(grpcServer, &mailboxes{})

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/events:pubsub", pubsubHandler)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := NewResponseWriter(w)

		defer func() {
			log.Debug().
				Str("protocol", r.Proto).
				Str("method", r.Method).
				Str("uri", r.URL.RequestURI()).
				Str("user_agent", r.UserAgent()).
				Str("referrer", r.Referer()).
				Uint32("status", rw.Status()).
				Uint64("response_size", rw.Size()).
				Dur("latency", time.Since(start)).
				Msg("http request")
		}()

		r = r.WithContext(log.Logger.WithContext(r.Context()))

		// Serve gRPC
		if r.ProtoMajor == 2 && strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc") {
			grpcServer.ServeHTTP(rw, r)
			return
		}

		// Serve http
		mux.ServeHTTP(rw, r)
	})

	return handler
}

func NewAuthnInterceptor(conn *grpc.ClientConn) grpc.UnaryServerInterceptor {
	client := tknz.NewAuthnClient(conn)

	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		log := zerolog.Ctx(ctx)

		meta, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			log.Error().Msg("missing metadata in incoming context")
			return nil, ErrInternal
		}

		var token string
		if v := meta.Get(metaAuthorizationKey); len(v) > 0 {
			token = v[0]
		}

		if token == "" {
			return nil, NewErrorf(ErrUnauthenticated, "missing credentials")
		}

		if !strings.HasPrefix(token, bearerTokenPrefix) {
			return nil, NewErrorf(ErrUnauthenticated, "invalid or unsupported credentials")
		}

		var appId *string
		if id := os.Getenv(envTknzAppIdKey); id != "" {
			appId = &id
		}

		token = token[len(bearerTokenPrefix):] // strip 'Bearer ' prefix
		resp, err := client.Check(ctx, &tknz.AuthnCheckRequest{AppId: appId, Token: token})
		if err != nil {
			log.Warn().Err(err).Msg("failed to check token")
			return nil, NewError(ErrUnauthenticated, err)
		}

		if !resp.GetOk() {
			return nil, NewError(ErrUnauthenticated, nil)
		}

		idn := resp.GetIdn()
		ctx = context.WithValue(ctx, CtxIdnKey{}, idn)
		ctx = context.WithValue(ctx, CtxSpaceIdKey{}, idn.GetSpaceId())

		return handler(ctx, req)
	}
}

func UnaryErrorInterceptor(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	resp, err := handler(ctx, req)
	if err == nil {
		return resp, nil
	}

	log := zerolog.Ctx(ctx)
	log.Debug().Err(err).Msg("")

	var c ErrorCode
	if errors.As(err, &c) {
		return resp, NewError(c, nil)
	}

	var e *Error
	if errors.As(err, &e) {
		return resp, err
	}

	if status.Convert(err).Code() == codes.Unimplemented {
		return resp, err
	}

	log.Warn().Err(err).Msg("unknown error")
	return resp, NewError(ErrUnknown, err)
}

func UnaryLoggerInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	log := zerolog.Ctx(ctx)
	start := time.Now()
	resp, err := handler(ctx, req)
	latency := time.Since(start)

	log.Debug().
		Dur("latency", latency).
		Str("rpc", info.FullMethod).
		Str("status", status.Code(err).String()).
		Msg("grpc request")

	return resp, err
}

func mapList[I any, O any](list []I, mapper func(I) O) []O {
	out := make([]O, 0, len(list))
	for _, s := range list {
		out = append(out, mapper(s))
	}

	return out
}
