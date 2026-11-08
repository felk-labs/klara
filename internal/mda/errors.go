package mda

import (
	"errors"

	"github.com/uatuko/go-smtp"
)

var (
	errInternal       = errors.New("internal error")
	errDeliveryFailed = errors.New("mail delivery failed for one or more recipients")

	errInvalidRecipientDomain = &smtp.SMTPError{
		Code:         553,
		EnhancedCode: smtp.EnhancedCode{5, 1, 2},
		Message:      "Recipient domain invalid or not allowed",
	}
)
