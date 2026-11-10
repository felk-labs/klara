package mta

import (
	"fmt"
	"os"
	"testing"

	"go.klara.mx/internal/db"
	"go.klara.mx/internal/kms"
)

func TestMain(m *testing.M) {
	// Setup
	if err := db.Setup(); err != nil {
		fmt.Printf("[error] db setup failed (%v)\n", err)
		os.Exit(1)
	}

	if err := kms.Setup(); err != nil {
		fmt.Printf("[error] kms setup failed (%v)\n", err)
		os.Exit(1)
	}

	// Run
	code := m.Run()

	// Teardown
	kms.Teardown()
	db.Teardown()

	os.Exit(code)
}
