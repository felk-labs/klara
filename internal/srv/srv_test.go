//go:build test

package srv

import (
	"fmt"
	"os"
	"testing"

	"github.com/klara-to/mx/internal/db"
	"github.com/klara-to/mx/internal/events"
)

func TestMain(m *testing.M) {
	// Setup
	if err := db.Setup(); err != nil {
		fmt.Printf("[error] db setup failed (%v)\n", err)
		os.Exit(1)
	}

	if err := events.Setup(); err != nil {
		fmt.Printf("[error] events setup failed (%v)\n", err)
		os.Exit(1)
	}

	// Run
	code := m.Run()

	// Teardown
	events.Teardown()
	db.Teardown()

	os.Exit(code)
}
