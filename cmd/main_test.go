package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"go.opentelemetry.io/otel/log/noop"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"

	urlshortenerv1alpha1 "github.com/spechtlabs/urlshortener/api/v1alpha1"
)

func TestParseFlags(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		logLevel  string
		wantBind  string
		wantDebug bool
	}{
		{name: "defaults", wantBind: ":8123"},
		{name: "the chart's flags", args: []string{"--health-probe-bind-address=:8081", "--bind-address=:9000", "--metrics-bind-address=:8082", "--debug"}, wantBind: ":9000", wantDebug: true},
		{name: "debug from OTEL_LOG_LEVEL", logLevel: "debug", wantBind: ":8123", wantDebug: true},
		{name: "an unknown flag", args: []string{"--no-such-flag"}, wantBind: ":8123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("OTEL_LOG_LEVEL", tt.logLevel)

			fs := flag.NewFlagSet("urlshortener", flag.ContinueOnError)
			fs.SetOutput(os.Stderr)

			opts := parseFlags(fs, tt.args)
			if opts.bindAddr != tt.wantBind || opts.debug != tt.wantDebug {
				t.Errorf("parseFlags() = bind %q, debug %v; want %q, %v", opts.bindAddr, opts.debug, tt.wantBind, tt.wantDebug)
			}

			if !opts.secureMetrics || opts.probeAddr != ":8081" {
				t.Errorf("parseFlags() changed a default: %+v", opts)
			}
		})
	}
}

func TestNewScheme(t *testing.T) {
	scheme, err := newScheme()
	if err != nil {
		t.Fatalf("newScheme() error = %v", err)
	}

	for _, gvk := range []string{"Redirect", "Shortlink", "Ingress"} {
		if !hasKind(scheme.AllKnownTypes(), gvk) {
			t.Errorf("newScheme() doesn't know %s", gvk)
		}
	}

	if !scheme.Recognizes(urlshortenerv1alpha1.GroupVersion.WithKind("Shortlink")) {
		t.Errorf("newScheme() doesn't recognize the Shortlink kind")
	}
}

func TestSetupLogging(t *testing.T) {
	for _, debug := range []bool{true, false} {
		undo, err := setupLogging(debug, noop.NewLoggerProvider())
		if err != nil {
			t.Fatalf("setupLogging(%v) error = %v", debug, err)
		}

		undo()
	}
}

func TestNewCertWatcher(t *testing.T) {
	tests := []struct {
		name        string
		dir         string
		wantWatcher bool
		wantErr     bool
	}{
		{name: "no directory"},
		{name: "a certificate", dir: certDir(t), wantWatcher: true},
		{name: "a directory without one", dir: t.TempDir(), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			watcher, err := newCertWatcher(tt.dir, "tls.crt", "tls.key")
			if (err != nil) != tt.wantErr {
				t.Fatalf("newCertWatcher() error = %v, wantErr %v", err, tt.wantErr)
			}

			if (watcher != nil) != tt.wantWatcher {
				t.Errorf("newCertWatcher() = %v, want a watcher: %v", watcher, tt.wantWatcher)
			}
		})
	}
}

func TestNewMetricsServerOptions(t *testing.T) {
	watcher, err := newCertWatcher(certDir(t), "tls.crt", "tls.key")
	if err != nil {
		t.Fatal(err)
	}

	tlsOpts := []func(*tls.Config){func(*tls.Config) {}}

	tests := []struct {
		name        string
		opts        options
		wantFilter  bool
		wantTLSOpts int
	}{
		{name: "secure", opts: options{metricsAddr: ":8443", secureMetrics: true}, wantFilter: true, wantTLSOpts: 1},
		{name: "plain HTTP", opts: options{metricsAddr: ":8080"}, wantTLSOpts: 1},
		{name: "with a certificate", opts: options{metricsAddr: ":8443", secureMetrics: true, metricsCertPath: "set"}, wantFilter: true, wantTLSOpts: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			certWatcher := watcher
			if tt.opts.metricsCertPath == "" {
				certWatcher = nil
			}

			got := newMetricsServerOptions(tt.opts, tlsOpts, certWatcher)
			if got.BindAddress != tt.opts.metricsAddr || got.SecureServing != tt.opts.secureMetrics {
				t.Errorf("newMetricsServerOptions() = %+v", got)
			}

			if (got.FilterProvider != nil) != tt.wantFilter {
				t.Errorf("FilterProvider set: %v, want %v", got.FilterProvider != nil, tt.wantFilter)
			}

			if len(got.TLSOpts) != tt.wantTLSOpts {
				t.Errorf("%d TLS options, want %d", len(got.TLSOpts), tt.wantTLSOpts)
			}
		})
	}
}

// TestNewManager creates managers for a cluster that doesn't exist: nothing
// reaches the cluster until the manager starts. setupRunnables registers the
// metrics with controller-runtime's global registry, so it succeeds once per
// process, which is what the second manager shows.
func TestNewManager(t *testing.T) {
	// The HTTP server loads its templates from html/ in the working directory.
	t.Chdir("..")

	cfg := &rest.Config{Host: "https://127.0.0.1:1"}

	if _, err := newManager(cfg, options{webhookCertPath: t.TempDir(), webhookCertName: "tls.crt", webhookCertKey: "tls.key"}); err == nil {
		t.Errorf("newManager() without the webhook certificate = nil, want an error")
	}

	if _, err := newManager(cfg, options{metricsCertPath: t.TempDir(), metricsCertName: "tls.crt", metricsCertKey: "tls.key"}); err == nil {
		t.Errorf("newManager() without the metrics certificate = nil, want an error")
	}

	if _, err := newManager(&rest.Config{Host: "::not a url"}, options{}); err == nil {
		t.Errorf("newManager() with an invalid config = nil, want an error")
	}

	dir := certDir(t)
	opts := options{
		metricsAddr:     "0",
		probeAddr:       "0",
		bindAddr:        "127.0.0.1:0",
		metricsCertPath: dir,
		metricsCertName: "tls.crt",
		metricsCertKey:  "tls.key",
		webhookCertPath: dir,
		webhookCertName: "tls.crt",
		webhookCertKey:  "tls.key",
	}

	mgr, err := newManager(cfg, opts)
	if err != nil {
		t.Fatalf("newManager() error = %v", err)
	}

	if setupErr := setupRunnables(mgr, opts.bindAddr); setupErr != nil {
		t.Fatalf("setupRunnables() error = %v", setupErr)
	}

	again, err := newManager(cfg, options{metricsAddr: "0", probeAddr: "0"})
	if err != nil {
		t.Fatalf("newManager() error = %v", err)
	}

	if err := setupRunnables(again, opts.bindAddr); err == nil {
		t.Errorf("setupRunnables() registered the metrics twice")
	}
}

func hasKind(types map[schema.GroupVersionKind]reflect.Type, kind string) bool {
	for gvk := range types {
		if gvk.Kind == kind {
			return true
		}
	}

	return false
}

// certDir writes a self-signed certificate and its key as tls.crt and
// tls.key into a new directory, and returns the directory.
func certDir(t *testing.T) string {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "urlshortener"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	writePEM(t, filepath.Join(dir, "tls.crt"), "CERTIFICATE", der)
	writePEM(t, filepath.Join(dir, "tls.key"), "EC PRIVATE KEY", keyDER)

	return dir
}

func writePEM(t *testing.T, path, blockType string, der []byte) {
	t.Helper()

	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
}
