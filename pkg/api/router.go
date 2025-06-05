package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/cedi/urlshortener/docs"
	"github.com/cedi/urlshortener/pkg/api/middleware"
	shortlinkClient "github.com/cedi/urlshortener/pkg/client"
	ginzap "github.com/gin-contrib/zap"
	"github.com/sierrasoftworks/humane-errors-go"

	"github.com/spechtlabs/go-gin-prometheus"
	"github.com/spechtlabs/go-otel-utils/otelzap"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	"github.com/gin-gonic/contrib/secure"
	"github.com/gin-gonic/gin"

	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"

	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// @title 			URL Shortener
// @version         2.0
// @description     A url shortener, written in Go running on Kubernetes
// @contact.name   Cedric Kienzler
// @contact.url    specht-labs.de
// @contact.email  urlshortener@specht-labs.de
// @license.name  	Apache 2.0
// @license.url   	http://www.apache.org/licenses/LICENSE-2.0.html
// @BasePath /
// @securityDefinitions.apiKey bearerAuth
// @in header
// @name Authorization

type UrlshortenerServer struct {
	srv        *http.Server
	router     *gin.Engine
	tracer     trace.Tracer
	userClient *shortlinkClient.UserShortLinkClient
	client     *shortlinkClient.ShortlinkClient
}

// NewGinGonicHTTPServer creates a new urlshortener API Server
func NewGinGonicHTTPServer(client *shortlinkClient.ShortlinkClient) *UrlshortenerServer {
	r := &UrlshortenerServer{
		srv:        nil,
		tracer:     otel.Tracer("urlshortener"),
		userClient: shortlinkClient.NewUserShortLinkClient(client),
		client:     client,
	}

	// Setup Gin router
	r.router = gin.New(func(e *gin.Engine) {})

	// Setup otelgin to expose Open Telemetry
	r.router.Use(otelgin.Middleware("urlshortener"))

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

	docs.SwaggerInfo.BasePath = "/"

	return r
}

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

func (s *UrlshortenerServer) ServeAsync(addr string) {
	go func() {
		if err := s.Serve(addr); err != nil {
			otelzap.L().WithError(err).Fatal("Unable to start proxy")
		}
	}()
}

func (s *UrlshortenerServer) Serve(addr string) humane.Error {
	otelzap.L().Info("Starting urlshortener server", zap.String("address", addr))

	// configure the HTTP Server
	s.srv = &http.Server{
		Addr:    addr,
		Handler: s.router,
	}

	if err := s.srv.ListenAndServe(); err != nil {
		if strings.Contains(err.Error(), http.ErrServerClosed.Error()) {
			otelzap.L().Info("API server stopped", zap.String("addr", s.srv.Addr))
			return nil
		}

		return humane.Wrap(err, "Unable to start API Server", "Make sure the api server is not already running and try again.")
	}

	return nil
}

func (s *UrlshortenerServer) Shutdown(ctx context.Context) humane.Error {
	if s.srv == nil {
		return humane.New("Unable to shutdown server. It is not running.", "Start server first before attempting to stop it")
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	otelzap.L().Info("shutting down proxy")
	if err := s.srv.Shutdown(timeoutCtx); err != nil {
		return humane.Wrap(err, "Unable to shutdown server", "Make sure the server is running and try again.")
	}

	return nil
}
