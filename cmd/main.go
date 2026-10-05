/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/go-logr/zapr"
	"github.com/sierrasoftworks/humane-errors-go"
	"github.com/spechtlabs/go-otel-utils/otelprovider"
	"github.com/spechtlabs/go-otel-utils/otelzap"
	"go.opentelemetry.io/otel/log"
	"go.uber.org/zap"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	urlshortenerv1alpha1 "github.com/spechtlabs/urlshortener/api/v1alpha1"
	"github.com/spechtlabs/urlshortener/internal/controller"
	apiController "github.com/spechtlabs/urlshortener/pkg/api"
	// +kubebuilder:scaffold:imports
)

// options holds the command line flags.
type options struct {
	metricsAddr     string
	probeAddr       string
	bindAddr        string
	metricsCertPath string
	metricsCertName string
	metricsCertKey  string
	webhookCertPath string
	webhookCertName string
	webhookCertKey  string

	enableLeaderElection bool
	secureMetrics        bool
	enableHTTP2          bool
	debug                bool
}

func main() {
	// flag.CommandLine, because controller-runtime registers --kubeconfig there.
	opts := parseFlags(flag.CommandLine, os.Args[1:])

	if err := run(opts); err != nil {
		humane.Eprint(err)
		os.Exit(1)
	}
}

// parseFlags parses args into options with fs. fs exits on a bad flag or
// -help, as flag.CommandLine does.
func parseFlags(fs *flag.FlagSet, args []string) options {
	var opts options

	fs.StringVar(&opts.metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	fs.StringVar(&opts.probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	fs.StringVar(&opts.bindAddr, "bind-address", ":8123", "The address the shortlink redirects and the API are served on.")
	fs.BoolVar(&opts.enableLeaderElection, "leader-elect", false, "Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	fs.BoolVar(&opts.secureMetrics, "metrics-secure", true, "If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	fs.StringVar(&opts.webhookCertPath, "webhook-cert-path", "", "The directory that contains the webhook certificate.")
	fs.StringVar(&opts.webhookCertName, "webhook-cert-name", "tls.crt", "The name of the webhook certificate file.")
	fs.StringVar(&opts.webhookCertKey, "webhook-cert-key", "tls.key", "The name of the webhook key file.")
	fs.StringVar(&opts.metricsCertPath, "metrics-cert-path", "", "The directory that contains the metrics server certificate.")
	fs.StringVar(&opts.metricsCertName, "metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	fs.StringVar(&opts.metricsCertKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	fs.BoolVar(&opts.enableHTTP2, "enable-http2", false, "If set, HTTP/2 will be enabled for the metrics and webhook servers")
	fs.BoolVar(&opts.debug, "debug", false, "Turn on debug logging")

	// fs exits on an error, unless a test made it continue on one.
	_ = fs.Parse(args)

	if !opts.debug {
		opts.debug = os.Getenv("OTEL_LOG_LEVEL") == "debug"
	}

	return opts
}

// run sets up logging and tracing, then runs the manager, with the
// reconcilers and the HTTP server, until SIGINT or SIGTERM.
func run(opts options) humane.Error {
	logProvider := otelprovider.NewLogger(
		otelprovider.WithLogAutomaticEnv(),
	)

	traceProvider := otelprovider.NewTracer(
		otelprovider.WithTraceAutomaticEnv(),
	)

	undoLogging, err := setupLogging(opts.debug, logProvider)
	if err != nil {
		return err
	}

	// Flush and stop the providers once the manager has stopped. A failure
	// only loses telemetry, so it's logged rather than returned.
	defer func() {
		flushCtx := context.Background()
		if err := errors.Join(
			traceProvider.ForceFlush(flushCtx),
			logProvider.ForceFlush(flushCtx),
			traceProvider.Shutdown(flushCtx),
			logProvider.Shutdown(flushCtx),
		); err != nil {
			otelzap.L().WithError(err).WarnContext(flushCtx, "failed to flush and shut down the telemetry providers",
				zap.String("component", "telemetry"),
			)
		}

		undoLogging()
	}()

	mgr, err := newManager(ctrl.GetConfigOrDie(), opts)
	if err != nil {
		return err
	}

	if err := setupRunnables(mgr, opts.bindAddr); err != nil {
		return err
	}

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		return humane.Wrap(err, "The manager stopped with an error",
			"Check that the --bind-address, --metrics-bind-address and --health-probe-bind-address ports are free and the cluster is reachable",
		)
	}

	return nil
}

// setupLogging replaces the global zap and otelzap loggers and redirects the
// standard library's log to zap. It returns a function that undoes all of it.
func setupLogging(debug bool, logProvider log.LoggerProvider) (func(), humane.Error) {
	var zapLogger *zap.Logger
	var err error
	if debug {
		zapLogger, err = zap.NewDevelopment()
		gin.SetMode(gin.DebugMode)
	} else {
		zapLogger, err = zap.NewProduction()
		gin.SetMode(gin.ReleaseMode)
	}

	if err != nil {
		return nil, humane.Wrap(err, "Failed to initialize the logger", "This is a bug in the logger configuration; please report it")
	}

	undoZapGlobals := zap.ReplaceGlobals(zapLogger)
	undoStdLogRedirect := zap.RedirectStdLog(zapLogger)

	otelZapLogger := otelzap.New(zapLogger,
		otelzap.WithCaller(true),
		otelzap.WithMinLevel(zap.InfoLevel),
		otelzap.WithAnnotateLevel(zap.WarnLevel),
		otelzap.WithErrorStatusLevel(zap.ErrorLevel),
		otelzap.WithStackTrace(false),
		otelzap.WithLoggerProvider(logProvider),
	)

	undoOtelZapGlobals := otelzap.ReplaceGlobals(otelZapLogger)

	ctrl.SetLogger(zapr.NewLogger(otelzap.L().Logger))

	return func() {
		undoStdLogRedirect()
		undoOtelZapGlobals()
		undoZapGlobals()
	}, nil
}

// newScheme returns the scheme with the Kubernetes built-in types and the
// urlshortener API types.
func newScheme() (*runtime.Scheme, humane.Error) {
	scheme := runtime.NewScheme()

	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return nil, humane.Wrap(err, "Failed to register the Kubernetes types", "This is a bug in the client-go scheme; please report it")
	}

	if err := urlshortenerv1alpha1.AddToScheme(scheme); err != nil {
		return nil, humane.Wrap(err, "Failed to register the urlshortener types", "This is a bug in api/v1alpha1; please report it")
	}
	// +kubebuilder:scaffold:scheme

	return scheme, nil
}

// newManager creates the controller manager for the cluster cfg points at, with
// its metrics and webhook servers configured from opts.
func newManager(cfg *rest.Config, opts options) (ctrl.Manager, humane.Error) {
	scheme, err := newScheme()
	if err != nil {
		return nil, err
	}

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	var tlsOpts []func(*tls.Config)
	if !opts.enableHTTP2 {
		tlsOpts = append(tlsOpts, func(c *tls.Config) {
			c.NextProtos = []string{"http/1.1"}
		})
	}

	webhookCertWatcher, err := newCertWatcher(opts.webhookCertPath, opts.webhookCertName, opts.webhookCertKey)
	if err != nil {
		return nil, err
	}

	webhookTLSOpts := tlsOpts
	if webhookCertWatcher != nil {
		webhookTLSOpts = append(webhookTLSOpts, func(config *tls.Config) {
			config.GetCertificate = webhookCertWatcher.GetCertificate
		})
	}

	metricsCertWatcher, err := newCertWatcher(opts.metricsCertPath, opts.metricsCertName, opts.metricsCertKey)
	if err != nil {
		return nil, err
	}

	mgr, mgrErr := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                 scheme,
		Metrics:                newMetricsServerOptions(opts, tlsOpts, metricsCertWatcher),
		WebhookServer:          webhook.NewServer(webhook.Options{TLSOpts: webhookTLSOpts}),
		HealthProbeBindAddress: opts.probeAddr,
		LeaderElection:         opts.enableLeaderElection,
		LeaderElectionID:       "772b19d3.cedi.dev",
	})
	if mgrErr != nil {
		return nil, humane.Wrap(mgrErr, "Unable to create the controller manager", "Check that the kubeconfig or in-cluster configuration points at a reachable cluster")
	}

	for _, watcher := range []*certwatcher.CertWatcher{metricsCertWatcher, webhookCertWatcher} {
		if watcher == nil {
			continue
		}

		if err := mgr.Add(watcher); err != nil {
			return nil, humane.Wrap(err, "Unable to add a certificate watcher to the manager", "The manager must not have been started yet")
		}
	}

	return mgr, nil
}

// newCertWatcher watches the certificate and key in dir, or returns nil when
// dir is empty.
func newCertWatcher(dir, certName, keyName string) (*certwatcher.CertWatcher, humane.Error) {
	if dir == "" {
		return nil, nil
	}

	watcher, err := certwatcher.New(filepath.Join(dir, certName), filepath.Join(dir, keyName))
	if err != nil {
		return nil, humane.Wrap(err, fmt.Sprintf("Failed to watch the certificate in %s", dir),
			fmt.Sprintf("Make sure %s and %s exist in %s and are readable", certName, keyName, dir),
		)
	}

	return watcher, nil
}

// newMetricsServerOptions configures the metrics endpoint from opts. More info:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/metrics/server
// - https://book.kubebuilder.io/reference/metrics.html
func newMetricsServerOptions(opts options, tlsOpts []func(*tls.Config), certWatcher *certwatcher.CertWatcher) metricsserver.Options {
	metricsOpts := metricsserver.Options{
		BindAddress:   opts.metricsAddr,
		SecureServing: opts.secureMetrics,
		TLSOpts:       tlsOpts,
	}

	if opts.secureMetrics {
		// FilterProvider is used to protect the metrics endpoint with authn/authz.
		// These configurations ensure that only authorized users and service accounts
		// can access the metrics endpoint. The RBAC are configured in 'config/rbac/kustomization.yaml'.
		metricsOpts.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	// Without a certificate, controller-runtime generates a self-signed one
	// for the metrics server, which is fine for development but not for
	// production.
	if certWatcher != nil {
		metricsOpts.TLSOpts = append(metricsOpts.TLSOpts, func(config *tls.Config) {
			config.GetCertificate = certWatcher.GetCertificate
		})
	}

	return metricsOpts
}

// setupRunnables registers the reconcilers, their metrics, the health and
// readiness checks and the HTTP server, serving on bindAddr, with mgr.
func setupRunnables(mgr ctrl.Manager, bindAddr string) humane.Error {
	if err := setupReconcilers(mgr); err != nil {
		return err
	}

	if err := addHealthChecks(mgr); err != nil {
		return err
	}

	// The HTTP server is one of the manager's runnables: it starts once the
	// caches it reads shortlinks from have synced, and the manager shuts it
	// down gracefully when it stops.
	srv := apiController.NewGinGonicHTTPServer(mgr.GetClient(), bindAddr)
	srv.Load()

	if err := mgr.Add(srv); err != nil {
		return humane.Wrap(err, "Unable to add the HTTP server to the manager", "The manager must not have been started yet")
	}

	return nil
}

// setupReconcilers registers the Redirect and Shortlink reconcilers and their
// metrics with mgr.
func setupReconcilers(mgr ctrl.Manager) humane.Error {
	if err := controller.RegisterMetrics(metrics.Registry); err != nil {
		return err
	}

	if err := controller.NewRedirectReconciler(mgr.GetClient(), mgr.GetScheme()).SetupWithManager(mgr); err != nil {
		return humane.Wrap(err, "Unable to create the Redirect controller", "Check the manager's scheme registers the urlshortener API types")
	}

	if err := controller.NewShortLinkReconciler(mgr.GetClient(), mgr.GetScheme()).SetupWithManager(mgr); err != nil {
		return humane.Wrap(err, "Unable to create the Shortlink controller", "Check the manager's scheme registers the urlshortener API types")
	}
	// +kubebuilder:scaffold:builder

	return nil
}

// addHealthChecks serves the liveness and readiness probes on the manager's
// health probe address.
func addHealthChecks(mgr ctrl.Manager) humane.Error {
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return humane.Wrap(err, "Unable to set up the health check", "Each check name may be registered only once")
	}

	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return humane.Wrap(err, "Unable to set up the ready check", "Each check name may be registered only once")
	}

	return nil
}
