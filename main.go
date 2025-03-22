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
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

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
	"github.com/uptrace/opentelemetry-go-extra/otelzap"

	v1alpha1 "github.com/cedi/urlshortener/api/v1alpha1"
	"github.com/cedi/urlshortener/controllers"
	shortlinkClient "github.com/cedi/urlshortener/pkg/client"
	apiController "github.com/cedi/urlshortener/pkg/controller"
	"github.com/cedi/urlshortener/pkg/observability"
	"github.com/cedi/urlshortener/pkg/router"

	"github.com/pkg/errors"
	//+kubebuilder:scaffold:imports
)

var (
	scheme         = runtime.NewScheme()
	serviceName    = "urlshortener"
	serviceVersion = "1.0.0"
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

	// Initialize Logging
	otelLogger, undo := observability.InitLogging(debug)
	defer otelLogger.Sync()
	defer undo()

	ctrl.SetLogger(zapr.NewLogger(otelzap.L().Logger))

	// Initialize Tracing (OpenTelemetry)
	traceProvider, tracer, err := observability.InitTracer(serviceName, serviceVersion)
	if err != nil {
		otelzap.L().Sugar().Errorw("failed initializing tracing",
			zap.Error(err),
		)
		os.Exit(1)
	}

	defer func() {
		if err := traceProvider.Shutdown(context.Background()); err != nil {
			otelzap.L().Sugar().Errorw("Error shutting down tracer provider",
				zap.Error(err),
			)
		}
	}()

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
		otelzap.L().Sugar().Errorw("unable to start urlshortener",
			zap.Error(err),
		)
		os.Exit(1)
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
		otelzap.L().Sugar().Errorw("unable to create controller",
			zap.Error(err),
			zap.String("controller", "ShortLink"),
		)
		os.Exit(1)
	}

	redirectReconciler := controllers.NewRedirectReconciler(
		mgr.GetClient(),
		rClient,
		mgr.GetScheme(),
		tracer,
	)

	if err = redirectReconciler.SetupWithManager(mgr); err != nil {
		span.RecordError(err)
		otelzap.L().Sugar().Errorw("unable to create controller",
			zap.Error(err),
			zap.String("controller", "Redirect"),
		)
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	span.End()

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		otelzap.L().Sugar().Errorw("unable to set up health check",
			zap.Error(err),
		)
		os.Exit(1)
	}

	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		otelzap.L().Sugar().Errorw("unable to set up ready check",
			zap.Error(err),
		)
		os.Exit(1)
	}

	// run our urlshortener mgr in a separate go routine
	go func() {
		otelzap.L().Info("starting urlshortener")

		if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
			otelzap.L().Sugar().Errorw("unable starting urlshortener",
				zap.Error(err),
			)
			os.Exit(1)
		}
	}()

	shortlinkController := apiController.NewShortlinkController(
		tracer,
		sClient,
	)

	// Init Gin Framework
	gin.SetMode(gin.ReleaseMode)
	r, srv := router.NewGinGonicHTTPServer(bindAddr, serviceName)

	otelzap.L().Info("Load API routes")
	router.Load(r, shortlinkController)

	// run our gin server mgr in a separate go routine
	go func() {
		if err := srv.ListenAndServe(); err != nil && errors.Is(err, http.ErrServerClosed) {
			otelzap.L().Sugar().Errorw("failed to listen and serve",
				zap.Error(err),
			)
		}
	}()

	handleShutdown(srv)

	otelzap.L().Info("Server exiting")
}

// handleShutdown waits for interrupt signal and then tries to gracefully
// shutdown the server with a timeout of 5 seconds.
func handleShutdown(srv *http.Server) {
	quit := make(chan os.Signal, 1)

	signal.Notify(
		quit,
		syscall.SIGINT,  // kill -2 is syscall.SIGINT
		syscall.SIGTERM, // kill (no param) default send syscall.SIGTERM
		// kill -9 is syscall.SIGKILL but can't be caught
	)

	// wait (and block) until shutdown signal is received
	<-quit
	otelzap.L().Info("Shutting down server...")

	// The context is used to inform the server it has 5 seconds to finish
	// the request it is currently handling
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// try to shut down the http server gracefully. If ctx deadline exceeds
	// then srv.Shutdown(ctx) will return an error, causing us to force
	// the shutdown
	if err := srv.Shutdown(ctx); err != nil {
		otelzap.L().Sugar().Errorw("Server forced to shutdown",
			zap.Error(err),
		)
		os.Exit(1)
	}
}
