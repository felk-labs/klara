//go:build test

package db

import (
	"context"
	"os"
	"strings"
	"time"
)

func Setup() error {
	dbname := os.Getenv("PGDATABASE")
	if !strings.HasPrefix(dbname, "test-") {
		os.Setenv("PGDATABASE", "test-"+dbname)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := Init(ctx); err != nil {
		return err
	}

	return nil
}

func Teardown() {
	Shutdown()
}
