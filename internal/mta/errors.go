package mta

import (
	"errors"

	"github.com/uatuko/go-smtp"
)

var (
	errInternal       = errors.New("internal error")
	errTransferFailed = errors.New("mail transfer failed for one or more recipients")

	errRelayInvalidCredentials = &smtp.SMTPError{
		Code:         550,
		EnhancedCode: smtp.EnhancedCode{5, 7, 0},
		Message:      "Invalid credentials for relay",
	}
)
