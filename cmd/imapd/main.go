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
	"github.com/uatuko/go-imap/server"

	"github.com/klara-to/mx/internal/db"
	"github.com/klara-to/mx/internal/storage"
)

var (
	addr   = flag.String("addr", ":1143", "tcp address to listen on")
	bucket = flag.String("bucket", "", "storage bucket id")
	debug  = flag.Bool("debug", false, "enable debug features")

	tlsCert = flag.String("tls-cert", "", "x509 signed certificate (e.g. /etc/tls/cert.pem)")
	tlsKey  = flag.String("tls-key", "", "tls private key (e.g. /etc/tls/key.pem)")
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

	if *bucket == "" {
		return fmt.Errorf("storage bucket required (e.g. -bucket=test)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := db.Init(ctx); err != nil {
		return fmt.Errorf("failed to initialise db, err: %w", err)
	}

	if err := storage.Init(ctx, *bucket); err != nil {
		return fmt.Errorf("failed to initialise storage, err: %w", err)
	}

	return nil
}

func main() {
	flag.Parse()

	err := _init()
	if err != nil {
		log.Fatal().Err(err).Msg("initialisation failed")
	}

	srv := server.New(&backend{})
	if *debug {
		srv.AllowInsecureAuth = true
		srv.Debug = os.Stdout
	}

	var l net.Listener
	if *tlsCert != "" && *tlsKey != "" {
		var x509 tls.Certificate
		x509, err = tls.LoadX509KeyPair(*tlsCert, *tlsKey)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to load tls cert or key")
		}

		srv.TLSConfig = &tls.Config{
			Certificates: []tls.Certificate{x509},
		}

		l, err = tls.Listen("tcp", *addr, srv.TLSConfig)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to listen")
		}
	} else {
		l, err = net.Listen("tcp", *addr)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to listen")
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
		log.Info().Msg("stopping imap server")
		if err := srv.Close(); err != nil {
			log.Fatal().Err(err).Msg("failed to stop imap server gracefully")
		}
	})

	log.Debug().Str("addr", l.Addr().String()).Msg("starting imap server")
	if err = srv.Serve(l); err != nil {
		log.Error().Err(err).Msg("imap server failed")
	}
}
