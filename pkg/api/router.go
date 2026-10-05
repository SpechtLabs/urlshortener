package api

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/sierrasoftworks/humane-errors-go"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ginzap "github.com/gin-contrib/zap"
	ginprometheus "github.com/spechtlabs/go-gin-prometheus"
	"github.com/spechtlabs/go-otel-utils/otelzap"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/spechtlabs/urlshortener/docs"
	"github.com/spechtlabs/urlshortener/pkg/api/middleware"
	shortlinkClient "github.com/spechtlabs/urlshortener/pkg/client"

	"sigs.k8s.io/controller-runtime/pkg/metrics"

	"github.com/gin-gonic/contrib/secure"
	"github.com/gin-gonic/gin"

	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

const (
	// readHeaderTimeout bounds how long a client may take to send its request
	// headers, so slow clients can't hold connections open (Slowloris).
	readHeaderTimeout = 10 * time.Second

	// shutdownTimeout is how long requests in flight get to finish on shutdown.
	shutdownTimeout = 5 * time.Second
)

// @title 			URL Shortener
// @version         2.0
// @description     A url shortener, written in Go running on Kubernetes
// @contact.name   Cedric Specht
// @contact.url    specht-labs.de
// @contact.email  urlshortener@specht-labs.de
// @license.name  	Apache 2.0
// @license.url   	http://www.apache.org/licenses/LICENSE-2.0.html
// @BasePath /
// @securityDefinitions.apiKey bearerAuth
// @in header
// @name Authorization

// UrlshortenerServer serves the shortlink redirects and the shortlink API.
type UrlshortenerServer struct {
	srv        *http.Server
	router     *gin.Engine
	tracer     trace.Tracer
	userClient *shortlinkClient.UserShortLinkClient
	client     *shortlinkClient.ShortlinkClient
}

// NewGinGonicHTTPServer creates a new urlshortener API Server, which serves on
// addr once started.
func NewGinGonicHTTPServer(k8sClient client.Client, addr string) *UrlshortenerServer {
	sClient := shortlinkClient.NewShortlinkClient(k8sClient)

	r := &UrlshortenerServer{
		tracer:     otel.Tracer("urlshortener"),
		userClient: shortlinkClient.NewUserShortLinkClient(sClient),
		client:     sClient,
	}

	// Setup Gin router
	r.router = gin.New(func(e *gin.Engine) {})

	// Setup otelgin to expose Open Telemetry
	r.router.Use(otelgin.Middleware("gin"))

	// Setup ginzap to log everything correctly to zap
	r.router.Use(ginzap.GinzapWithConfig(otelzap.L(), &ginzap.Config{
		UTC:        true,
		TimeFormat: time.RFC3339,
		Context: func(c *gin.Context) []zapcore.Field {
			var fields []zapcore.Field
			// log request ID
			if requestID := c.Writer.Header().Get("X-Request-Id"); requestID != "" {
				fields = append(fields, zap.String("request_id", requestID))
			}

			// log trace and span ID
			if spanContext := trace.SpanFromContext(c.Request.Context()).SpanContext(); spanContext.IsValid() {
				fields = append(fields, zap.String("trace_id", spanContext.TraceID().String()))
				fields = append(fields, zap.String("span_id", spanContext.SpanID().String()))
			}
			return fields
		},
	}))

	r.router.Use(
		secure.Secure(secure.Options{
			SSLRedirect:           true,
			SSLProxyHeaders:       map[string]string{"X-Forwarded-Proto": "https"},
			STSIncludeSubdomains:  true,
			FrameDeny:             true,
			ContentTypeNosniff:    true,
			BrowserXssFilter:      true,
			ContentSecurityPolicy: "default-src 'self' data: 'unsafe-inline'",
		}),
	)

	// load html file
	r.router.LoadHTMLGlob("html/templates/*.html")

	// static path
	r.router.Static("assets", "./html/assets")

	// Set-up exporter to expose prometheus metrics
	r.router.Use(ginprometheus.GinPrometheusMiddleware(r.router, "gin",
		ginprometheus.WithRegisterer(metrics.Registry),
		ginprometheus.WithLowCardinalityUrl(),
	))

	r.srv = &http.Server{
		Addr:              addr,
		Handler:           r.router,
		ReadHeaderTimeout: readHeaderTimeout,
	}

	docs.SwaggerInfo.BasePath = "/"

	return r
}

// Load registers the routes: the shortlink redirects and Swagger UI, which are
// public, and the API, which requires a GitHub token.
func (s *UrlshortenerServer) Load() {
	router := s.router

	// ------------------------------------------------------------------------
	// PUBLICLY ACCESSIBLE ENDPOINTS
	// ------------------------------------------------------------------------

	// Swagger Files
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// Short link Endpoint that triggers the redirect
	router.GET("/:shortlink", s.HandleShortLink)

	// ------------------------------------------------------------------------
	// AUTHENTICATED ENDPOINTS
	// ------------------------------------------------------------------------

	// API routes
	api := router.Group("/api")
	api.Use(middleware.GitHubUserAuthMiddleware())

	// v1 API
	v1 := api.Group("/v1")
	v1.GET("/shortlink/", s.HandleListShortLink)
	v1.GET("/shortlink/:shortlink", s.HandleGetShortLink)
	v1.POST("/shortlink/:shortlink", s.HandleCreateShortLink)
	v1.PUT("/shortlink/:shortlink", s.HandleUpdateShortLink)
	v1.DELETE("/shortlink/:shortlink", s.HandleDeleteShortLink)
}

// Start serves the shortlink redirects and the API until ctx is done, then
// shuts the server down, giving requests in flight shutdownTimeout to
// finish. It implements controller-runtime's manager.Runnable, so the
// manager starts it once its caches have synced and stops it with the
// manager.
func (s *UrlshortenerServer) Start(ctx context.Context) error {
	// The server goroutine ends when ListenAndServe fails or when Shutdown below
	// stops it; Start waits for it either way.
	var serving sync.WaitGroup
	defer serving.Wait()

	otelzap.L().InfoContext(ctx, "serving shortlinks and the API", zap.String("address", s.srv.Addr))

	serveErr := make(chan error, 1)
	serving.Go(func() {
		serveErr <- s.srv.ListenAndServe()
	})

	select {
	case err := <-serveErr:
		return humane.Wrap(err, fmt.Sprintf("Unable to serve on %s", s.srv.Addr),
			"Make sure no other process listens on the --bind-address and try again.",
		)

	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()

	if err := s.srv.Shutdown(shutdownCtx); err != nil {
		return humane.Wrap(err, "Unable to shut the server down gracefully",
			fmt.Sprintf("Requests still in flight after %s were cut off.", shutdownTimeout),
		)
	}

	otelzap.L().DebugContext(ctx, "server stopped", zap.String("address", s.srv.Addr))

	return nil
}

// NeedLeaderElection reports that every replica serves, not only the leader.
// It implements controller-runtime's manager.LeaderElectionRunnable.
func (s *UrlshortenerServer) NeedLeaderElection() bool {
	return false
}
