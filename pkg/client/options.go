package client

// Option configures a ShortlinkClient or a RedirectClient.
type Option func(*settings)

// settings are what the options configure.
type settings struct {
	namespaceFile string
}

// WithNamespaceFile makes the client read the namespace it works in from path
// rather than from the pod's service account, so it also works outside a
// pod.
func WithNamespaceFile(path string) Option {
	return func(s *settings) {
		s.namespaceFile = path
	}
}

func newSettings(opts []Option) settings {
	s := settings{namespaceFile: serviceAccountNamespaceFile}
	for _, opt := range opts {
		opt(&s)
	}

	return s
}
