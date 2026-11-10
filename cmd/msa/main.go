package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/uatuko/go-smtp"

	"go.klara.mx/internal/db"
	"go.klara.mx/internal/events"
)

var (
	addr   = flag.String("addr", ":1587", "tcp address to listen on")
	debug  = flag.Bool("debug", false, "enable debug logs")
	domain = flag.String("domain", "localhost", "smtp server domain")

	pubsubProject = flag.String("pubsub-project", "", "pub/sub project id")
	pubsubTopic   = flag.String("pubsub-topic", "", "pub/sub topic")

	tlsCert = flag.String("tls-cert", "", "x509 signed certificate (e.g. /etc/tls/cert.pem)")
	tlsKey  = flag.String("tls-key", "", "tls private key (e.g. /etc/tls/key.pem)")

	// Message size limit
	maxMessageBytes = flag.Int64("max-message-bytes", 1024*1024*10, "smtp message size limit in bytes")

	// Recipients buffer size, must be at least 100
	// Ref: https://www.rfc-editor.org/rfc/rfc5321.html#section-4.5.3.1.8
	maxRecipients = flag.Int("max-recipients", 100, "smtp recipients buffer size, must be at least 100")
)

func _init() error {
	zerolog.LevelFieldName = "severity"
	zerolog.TimeFieldFormat = time.RFC3339Nano
	zerolog.SetGlobalLevel(zerolog.InfoLevel)

	if *debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
		log.Logger = log.Output(zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: "15:04:05.000000",
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	err := db.Init(ctx)
	if err != nil {
		return fmt.Errorf("failed to initialise db, err: %w", err)
	}

	if err = events.Init(ctx, *pubsubProject, *pubsubTopic); err != nil {
		return fmt.Errorf("failed to initialise events, err: %w", err)
	}

	return nil
}

func main() {
	flag.Parse()

	if err := _init(); err != nil {
		log.Fatal().Err(err).Msg("initialisation failed")
	}

	l, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to listen")
	}

	srv := smtp.NewServer(&backend{})
	srv.Domain = *domain
	srv.MaxMessageBytes = *maxMessageBytes
	srv.MaxRecipients = *maxRecipients
	if *debug {
		srv.Debug = os.Stdout
		srv.AllowInsecureAuth = true
	}

	if *tlsCert != "" && *tlsKey != "" {
		var x509 tls.Certificate
		x509, err = tls.LoadX509KeyPair(*tlsCert, *tlsKey)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to load tls cert or key")
		}

		srv.TLSConfig = &tls.Config{
			Certificates: []tls.Certificate{x509},
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	context.AfterFunc(ctx, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		log.Info().Msg("stopping smtp server")
		if err := srv.Shutdown(ctx); err != nil {
			log.Fatal().Err(err).Msg("failed to stop smtp server gracefully")
		}

		events.Stop()
	})

	log.Debug().Str("addr", l.Addr().String()).Msg("starting smtp server")
	if err = srv.Serve(l); err != nil {
		log.Error().Err(err).Msg("smtp server failed")
	}
}
