package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/klara-to/mx/internal/db"
	"github.com/klara-to/mx/internal/events"
	"github.com/klara-to/mx/internal/kms"
	"github.com/klara-to/mx/internal/srv"
	"github.com/klara-to/mx/internal/storage"
)

var (
	addr          = flag.String("addr", ":50051", "tcp address to listen on")
	bucket        = flag.String("bucket", "", "storage bucket id")
	debug         = flag.Bool("debug", false, "enable debug logs")
	kmsKeyring    = flag.String("kms-keyring", "", "kms keyring resource name")
	kmsProject    = flag.String("kms-project", "", "kms project id")
	pubsubProject = flag.String("pubsub-project", "", "pub/sub project id")
	pubsubTopic   = flag.String("pubsub-topic", "", "pub/sub topic")
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

	if err = kms.Init(ctx, *kmsProject, *kmsKeyring); err != nil {
		return fmt.Errorf("failed to initialise kms, err: %w", err)
	}

	if err := storage.Init(ctx, *bucket); err != nil {
		return fmt.Errorf("failed to initialise storage, err: %w", err)
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

	srv := http.Server{
		Handler: srv.New(),
	}
	srv.Protocols = &http.Protocols{}
	srv.Protocols.SetHTTP1(true)
	srv.Protocols.SetUnencryptedHTTP2(true)

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

		log.Info().Msg("stopping http server")
		if err := srv.Shutdown(ctx); err != nil {
			log.Fatal().Err(err).Msg("failed to stop http server gracefully")
		}

		events.Stop()
	})

	log.Info().Str("addr", l.Addr().String()).Msg("starting http server")
	if err := srv.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal().Err(err).Msg("http server failed")
	}
}
