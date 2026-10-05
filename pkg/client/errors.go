package client

import (
	"fmt"
	"os"

	"github.com/sierrasoftworks/humane-errors-go"
)

// CRUDOperation names what a user tried to do to a ShortLink, for the error
// that tells them they may not.
type CRUDOperation string

const (
	// CreateOperation is creating a ShortLink.
	CreateOperation CRUDOperation = "create"
	// ReadOperation is reading or listing ShortLinks.
	ReadOperation CRUDOperation = "read"
	// UpdateOperation is changing a ShortLink.
	UpdateOperation CRUDOperation = "update"
	// DeleteOperation is deleting a ShortLink.
	DeleteOperation CRUDOperation = "delete"
)

// serviceAccountNamespaceFile is where Kubernetes mounts the namespace of the
// pod's service account.
const serviceAccountNamespaceFile = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"

// NewNotAllowedError returns the error for a user who doesn't own the ShortLink
// they tried to act on.
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

// currentNamespace returns the namespace the server runs in, read from
// namespaceFile (serviceAccountNamespaceFile unless WithNamespaceFile says
// otherwise).
func currentNamespace(namespaceFile string) (string, humane.Error) {
	// The path is the service account's namespace file, or the one the
	// operator named with WithNamespaceFile.
	namespace, err := os.ReadFile(namespaceFile) //nolint:gosec // G304: see above
	if err != nil {
		return "", humane.Wrap(err, "Unable to read current namespace",
			fmt.Sprintf("The server reads its namespace from %s, which Kubernetes mounts into every pod with a service account; run it in a pod, or use the *Namespaced methods", namespaceFile),
		)
	}

	return string(namespace), nil
}
