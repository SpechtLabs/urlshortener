/*
Copyright 2022.

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
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spechtlabs/go-otel-utils/otelprovider"
	"github.com/spechtlabs/go-otel-utils/otelzap"
	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/runtime"
	utilRuntime "k8s.io/apimachinery/pkg/util/runtime"
	clientGoScheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/gin-gonic/gin"
	"github.com/go-logr/zapr"

	"github.com/cedi/urlshortener/api/v1alpha1"
	"github.com/cedi/urlshortener/controllers"
	apiController "github.com/cedi/urlshortener/pkg/api"
	shortlinkClient "github.com/cedi/urlshortener/pkg/client"
	//+kubebuilder:scaffold:imports
)

var (
	scheme         = runtime.NewScheme()
	serviceVersion = "1.0.0"
	serviceName    = "urlshortener"
)

func init() {
	utilRuntime.Must(clientGoScheme.AddToScheme(scheme))

	utilRuntime.Must(v1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

// @title 			URL Shortener
// @version         1.0
// @description     A url shortener, written in Go running on Kubernetes

// @contact.name   Cedric Kienzler
// @contact.url    cedi.dev
// @contact.email  urlshortener@cedi.dev

// @license.name  	Apache 2.0
// @license.url   	http://www.apache.org/licenses/LICENSE-2.0.html
// @BasePath /
func main() {
	var metricsAddr string
	var probeAddr string
	var bindAddr string
	var namespaced bool
	var debug bool

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":9110", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":9081", "The address the probe endpoint binds to.")
	flag.StringVar(&bindAddr, "bind-address", ":8443", "The address the service binds to.")
	flag.BoolVar(&namespaced, "namespaced", true, "Restrict the urlshortener to only list resources in the current namespace")
	flag.BoolVar(&debug, "debug", false, "Turn on debug logging")

	flag.Parse()

	ctx, cancelCtx := context.WithCancelCause(context.Background())
	defer cancelCtx(context.Canceled)

	logProvider := otelprovider.NewLogger(
		otelprovider.WithLogAutomaticEnv(),
	)

	traceProvider := otelprovider.NewTracer(
		otelprovider.WithTraceAutomaticEnv(),
	)

	if !debug {
		debug = os.Getenv("OTEL_LOG_LEVEL") == "debug"
	}

	// Initialize Logging
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
		fmt.Printf("failed to initialize logger: %v", err)
		os.Exit(1)
	}

	// Replace zap global
	undoZapGlobals := zap.ReplaceGlobals(zapLogger)

	// Redirect stdlib log to zap
	undoStdLogRedirect := zap.RedirectStdLog(zapLogger)

	// Create otelLogger
	otelZapLogger := otelzap.New(zapLogger,
		otelzap.WithCaller(true),
		otelzap.WithMinLevel(zap.InfoLevel),
		otelzap.WithAnnotateLevel(zap.WarnLevel),
		otelzap.WithErrorStatusLevel(zap.ErrorLevel),
		otelzap.WithStackTrace(false),
		otelzap.WithLoggerProvider(logProvider),
	)

	// Replace global otelZap logger
	undoOtelZapGlobals := otelzap.ReplaceGlobals(otelZapLogger)

	defer func() {
		if err := traceProvider.ForceFlush(context.Background()); err != nil {
			otelzap.L().Warn("failed to flush traces")
		}

		if err := logProvider.ForceFlush(context.Background()); err != nil {
			otelzap.L().Warn("failed to flush logs")
		}

		if err := traceProvider.Shutdown(context.Background()); err != nil {
			panic(err)
		}

		if err := logProvider.Shutdown(context.Background()); err != nil {
			panic(err)
		}

		undoStdLogRedirect()
		undoOtelZapGlobals()
		undoZapGlobals()
	}()

	ctrl.SetLogger(zapr.NewLogger(otelzap.L().Logger))

	tracer := traceProvider.Tracer("urlshortener")
	_, span := tracer.Start(context.Background(), "main.startManager")

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: bindAddr,
		},
		HealthProbeBindAddress:        probeAddr,
		LeaderElection:                false,
		LeaderElectionID:              "a9a252fc.cedi.dev",
		LeaderElectionReleaseOnCancel: false,
	})

	if err != nil {
		span.RecordError(err)
		otelzap.L().WithError(err).Ctx(ctx).Fatal("unable to start urlshortener")
	}

	sClient := shortlinkClient.NewShortlinkClient(
		mgr.GetClient(),
		tracer,
	)

	rClient := shortlinkClient.NewRedirectClient(
		mgr.GetClient(),
		tracer,
	)

	shortlinkReconciler := controllers.NewShortLinkReconciler(
		sClient,
		mgr.GetScheme(),
		tracer,
	)

	if err = shortlinkReconciler.SetupWithManager(mgr); err != nil {
		span.RecordError(err)
		otelzap.L().WithError(err).Ctx(ctx).Fatal("unable to create api")
	}

	redirectReconciler := controllers.NewRedirectReconciler(
		mgr.GetClient(),
		rClient,
		mgr.GetScheme(),
		tracer,
	)

	if err = redirectReconciler.SetupWithManager(mgr); err != nil {
		span.RecordError(err)
		otelzap.L().Sugar().Errorw("unable to create api",
			zap.Error(err),
			zap.String("api", "Redirect"),
		)
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	span.End()

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		otelzap.L().WithError(err).Ctx(ctx).Fatal("unable to set up health check")
	}

	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		otelzap.L().WithError(err).Ctx(ctx).Fatal("unable to set up ready check")
	}

	// run our urlshortener mgr in a separate go routine
	go func() {
		otelzap.L().Info("starting urlshortener")

		if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
			otelzap.L().WithError(err).Ctx(ctx).Fatal("unable starting urlshortener")
		}
	}()

	// Init Gin Framework
	gin.SetMode(gin.ReleaseMode)
	srv := apiController.NewGinGonicHTTPServer(sClient)

	otelzap.L().Info("Load API routes")
	srv.Load()

	srv.ServeAsync(bindAddr)

	// setup stop signal handlers
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	go func() {
		select {
		// Wait for context cancel
		case <-ctx.Done():

		// Wait for signal
		case sig := <-sigs:
			switch sig {
			case syscall.SIGTERM:
				fallthrough
			case syscall.SIGINT:
				fallthrough
			case syscall.SIGQUIT:
				// On terminate signal, cancel context causing the program to terminate
				cancelCtx(fmt.Errorf("signal %s received", sig))

			default:
				otelzap.L().Ctx(ctx).Warn("Received unknown signal", zap.String("signal", sig.String()))
			}
		}
	}()

	// Wait for context to be done
	<-ctx.Done()

	// The context is used to inform the server it has 5 seconds to finish
	// the request it is currently handling
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	otelzap.L().Info("Shutting down server...")

	// try to shut down the http server gracefully. If ctx deadline exceeds
	// then srv.Shutdown(ctx) will return an error, causing us to force
	// the shutdown
	if err := srv.Shutdown(ctx); err != nil {
		otelzap.L().WithError(err).Error("Server forced to shutdown")
		os.Exit(1)
	}

	// Wait for context cancel
	if err := ctx.Err(); !errors.Is(err, context.Canceled) {
		otelzap.L().WithError(err).Fatal("Exiting")
	} else {
		otelzap.L().Info("Exiting")
	}
}
