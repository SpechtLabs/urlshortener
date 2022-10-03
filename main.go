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
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	"k8s.io/apimachinery/pkg/runtime"
	utilRuntime "k8s.io/apimachinery/pkg/util/runtime"
	clientGoScheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/gin-gonic/gin"
	"github.com/go-logr/logr"

	v1alpha1 "github.com/av0de/urlshortener/api/v1alpha1"
	"github.com/av0de/urlshortener/controllers"
	shortlinkClient "github.com/av0de/urlshortener/pkg/client"
	urlShortenerController "github.com/av0de/urlshortener/pkg/controller"
	router "github.com/av0de/urlshortener/pkg/router"
	tracing "github.com/av0de/urlshortener/pkg/tracing"
	//+kubebuilder:scaffold:imports
)

var (
	scheme         = runtime.NewScheme()
	setupLog       = ctrl.Log.WithName("setup")
	serviceName    = "urlshortener"
	serviceVersion = "1.0.0"
)

func init() {
	utilRuntime.Must(clientGoScheme.AddToScheme(scheme))

	utilRuntime.Must(v1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var probeAddr string
	var bindAddr string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":9110", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":9081", "The address the probe endpoint binds to.")
	flag.StringVar(&bindAddr, "bind-address", ":8443", "The address the service binds to.")
	opts := zap.Options{
		Development: false, // ToDo: Set to false to switch to JSON log format
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	shutdownLog := ctrl.Log.WithName("shutdown")

	o11y, err := tracing.NewShortlinkObservability(serviceName, serviceVersion, ctrl.Log)
	if err != nil {
		setupLog.Error(err, "failed initializing observability")
		os.Exit(1)
	}

	defer func() {
		if err := o11y.ShutdownTraceProvider(context.Background()); err != nil {
			shutdownLog.Error(err, "Error shutting down tracer provider")
		}
	}()

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                        scheme,
		MetricsBindAddress:            metricsAddr,
		Port:                          9443,
		HealthProbeBindAddress:        probeAddr,
		LeaderElection:                false,
		LeaderElectionID:              "a9a252fc.av0.de",
		LeaderElectionReleaseOnCancel: false,
	})
	if err != nil {
		setupLog.Error(err, "unable to start urlshortener")
		os.Exit(1)
	}

	sClient := shortlinkClient.NewShortlinkClient(
		mgr.GetClient(),
		o11y,
	)

	rClient := shortlinkClient.NewRedirectClient(
		mgr.GetClient(),
		o11y,
	)

	shortlinkReconciler := controllers.NewShortLinkReconciler(
		sClient,
		mgr.GetScheme(),
		o11y,
	)
	if err = shortlinkReconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ShortLink")
		os.Exit(1)
	}
	redirectReconciler := controllers.NewRedirectReconciler(
		mgr.GetClient(),
		rClient,
		mgr.GetScheme(),
		o11y,
	)
	if err = redirectReconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Redirect")
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	// run our urlshortener mgr in a separate go routine
	go func() {
		setupLog.Info("starting urlshortener")
		if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
			setupLog.Error(err, "problem running urlshortener")
			os.Exit(1)
		}
	}()

	shortlinkController := urlShortenerController.NewShortlinkController(o11y, sClient)

	// Init Gin Framework
	gin.SetMode(gin.ReleaseMode)
	r, srv := router.NewGinGonicHTTPServer(&setupLog, bindAddr)

	setupLog.Info("Load API routes")
	router.Load(r, shortlinkController)

	// run our gin server mgr in a separate go routine
	go func() {
		if err := srv.ListenAndServe(); err != nil && errors.Is(err, http.ErrServerClosed) {
			setupLog.Error(err, "listen\n")
		}
	}()

	handleShutdown(srv, &shutdownLog)

	shutdownLog.Info("Server exiting")
}

// handleShutdown waits for interrupt signal and then tries to gracefully
// shutdown the server with a timeout of 5 seconds.
func handleShutdown(srv *http.Server, shutdownLog *logr.Logger) {
	quit := make(chan os.Signal, 1)

	signal.Notify(
		quit,
		syscall.SIGINT,  // kill -2 is syscall.SIGINT
		syscall.SIGTERM, // kill (no param) default send syscall.SIGTERM
		// kill -9 is syscall.SIGKILL but can't be caught
	)

	// wait (and block) until shutdown signal is received
	<-quit
	shutdownLog.Info("Shutting down server...")

	// The context is used to inform the server it has 5 seconds to finish
	// the request it is currently handling
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// try to shut down the http server gracefully. If ctx deadline exceeds
	// then srv.Shutdown(ctx) will return an error, causing us to force
	// the shutdown
	if err := srv.Shutdown(ctx); err != nil {
		shutdownLog.Error(err, "Server forced to shutdown")
		os.Exit(1)
	}
}
