package api

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestServer starts the server the way the manager does, checks that it
// serves the routes Load registers, and stops it by canceling its context.
// NewGinGonicHTTPServer registers its metrics with controller-runtime's
// global registry, so this is the only test that creates one.
func TestServer(t *testing.T) {
	// The server loads its templates from html/ in the working directory.
	t.Chdir("../..")

	addr := freeAddr(t)
	srv := NewGinGonicHTTPServer(fake.NewClientBuilder().Build(), addr)
	srv.Load()

	if srv.NeedLeaderElection() {
		t.Errorf("NeedLeaderElection() = true; every replica must serve")
	}

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)
	go func() { stopped <- srv.Start(ctx) }()

	// Plain HTTP is redirected to HTTPS, unless a proxy in front terminated
	// TLS. Behind one, the API answers 401 without a token, before asking
	// GitHub anything.
	url := "http://" + addr + "/api/v1/shortlink/"
	if status := getStatus(t, url, ""); status != http.StatusMovedPermanently {
		t.Errorf("GET %s = %d, want %d", url, status, http.StatusMovedPermanently)
	}

	if status := getStatus(t, url, "https"); status != http.StatusUnauthorized {
		t.Errorf("GET %s behind a TLS proxy = %d, want %d", url, status, http.StatusUnauthorized)
	}

	cancel()

	select {
	case err := <-stopped:
		if err != nil {
			t.Errorf("Start() after cancel = %v, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Start() didn't return after its context was canceled")
	}

	// A second server can't bind the address while one listens there.
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()

	busy := &UrlshortenerServer{srv: &http.Server{Addr: addr, ReadHeaderTimeout: time.Second}}
	if err := busy.Start(context.Background()); err == nil {
		t.Errorf("Start() on a busy address = nil, want an error")
	}
}

// freeAddr returns a local address no one listens on.
func freeAddr(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	return addr
}

// getStatus GETs url, with forwardedProto as X-Forwarded-Proto if it's set,
// until the server answers, and returns the status without following a
// redirect.
func getStatus(t *testing.T, url, forwardedProto string) int {
	t.Helper()

	// Without keep-alives, no connection outlives its request, so none holds
	// up the server's graceful shutdown.
	client := &http.Client{
		Transport: &http.Transport{DisableKeepAlives: true},
		Timeout:   5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	deadline := time.Now().Add(10 * time.Second)
	for {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, http.NoBody)
		if err != nil {
			t.Fatal(err)
		}

		if forwardedProto != "" {
			req.Header.Set("X-Forwarded-Proto", forwardedProto)
		}

		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			return resp.StatusCode
		}

		if time.Now().After(deadline) {
			t.Fatalf("GET %s: %v", url, err)
		}

		time.Sleep(50 * time.Millisecond)
	}
}
