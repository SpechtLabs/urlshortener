package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/sierrasoftworks/humane-errors-go"
	"github.com/spechtlabs/go-otel-utils/otelzap"
	"go.uber.org/zap"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
)

// The operations the API logs its errors under.
const (
	operationCreate = "create"
	operationDelete = "delete"
	operationGet    = "get"
	operationList   = "list"
	operationUpdate = "update"
)

// newNoUserError is the error for an API request the auth middleware found
// no GitHub user for.
func newNoUserError() humane.Error {
	return humane.New("No user found for request",
		"ensure you include a Bearer token in the Authorization header, e.g. Authorization: Bearer <token> or Authorization: token <token>",
	)
}

// abortWithError logs err as the request's error event and answers it with
// status and a JSON body holding err's message and advice.
func abortWithError(ct *gin.Context, status int, operation string, err humane.Error) {
	otelzap.L().WithError(err).ErrorContext(ct.Request.Context(), err.Error(),
		zap.String("shortlink", ct.Param("shortlink")),
		zap.String("operation", operation),
		zap.Int("status", status),
	)

	ct.AbortWithStatusJSON(status, gin.H{"error": err.Error(), "advice": err.Advice()})
}

// statusFor returns the HTTP status for an error from the ShortLink client:
// 404 when the ShortLink doesn't exist, 500 for everything else.
func statusFor(err error) int {
	if k8serrors.IsNotFound(err) {
		return http.StatusNotFound
	}

	return http.StatusInternalServerError
}

// renderError logs err as the request's error event and answers a shortlink
// request with the error page for status: 404.html when the ShortLink doesn't
// exist, which is routine and logged at debug level, and 500.html otherwise.
func renderError(ct *gin.Context, status int, err humane.Error) {
	fields := []zap.Field{
		zap.String("shortlink", ct.Param("shortlink")),
		zap.String("operation", "shortlink"),
		zap.Int("status", status),
	}

	if status == http.StatusNotFound {
		otelzap.L().WithError(err).DebugContext(ct.Request.Context(), err.Error(), fields...)
		ct.HTML(http.StatusNotFound, "404.html", gin.H{})
		return
	}

	otelzap.L().WithError(err).ErrorContext(ct.Request.Context(), err.Error(), fields...)
	ct.HTML(http.StatusInternalServerError, "500.html", gin.H{})
}
