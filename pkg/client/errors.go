package client

import (
	"fmt"

	"github.com/sierrasoftworks/humane-errors-go"
)

type CRUDOperation string

const (
	CreateOperation CRUDOperation = "create"
	ReadOperation   CRUDOperation = "read"
	UpdateOperation CRUDOperation = "update"
	DeleteOperation CRUDOperation = "delete"
)

func NewNotAllowedError(username string, operation CRUDOperation, shortlinkName string) humane.Error {
	return humane.New(
		fmt.Sprintf("Operation '%s' for user '%s' is not allowed for ShortLink '%s'",
			operation,
			username,
			shortlinkName,
		),
		"ensure you have the correct permissions to perform this operation on the ShortLink.",
	)
}
