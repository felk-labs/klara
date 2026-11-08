package main

const (
	errInvalidCredentials errorMsg = "invalid credentials"
	errNotImplemented     errorMsg = "not implemented"
	errUnknown            errorMsg = "unknown error"

	errMailboxNotFound      errorMsg = "mailbox not found"
	errMailboxAlreadyExists errorMsg = "mailbox already exists"

	errInvalidMessageData errorMsg = "invalid message data"
)

type errorMsg string

func (e errorMsg) Error() string {
	return string(e)
}
