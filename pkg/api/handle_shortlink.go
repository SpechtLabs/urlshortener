package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/spechtlabs/go-otel-utils/otelzap"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// HandleShortLink handles the shortlink and redirects according to the configuration
// @BasePath /
// @Summary       redirect to target
// @Schemes       http https
// @Description   redirect to target as per configuration of the shortlink
// @Produce       text/html
// @Param         shortlink   path      string  true  "shortlink id"
// @Success       200         {object}  int     "Success"
// @Success       300         {object}  int     "MultipleChoices"
// @Success       301         {object}  int     "MovedPermanently"
// @Success       302         {object}  int     "Found"
// @Success       303         {object}  int     "SeeOther"
// @Success       304         {object}  int     "NotModified"
// @Success       305         {object}  int     "UseProxy"
// @Success       307         {object}  int     "TemporaryRedirect"
// @Success       308         {object}  int     "PermanentRedirect"
// @Failure       404         {object}  int     "NotFound"
// @Failure       500         {object}  int     "InternalServerError"
// @Tags default
// @Router /{shortlink} [get]
func (s *UrlshortenerServer) HandleShortLink(ct *gin.Context) {
	shortlinkName := ct.Param("shortlink")

	ctx := ct.Request.Context()
	span := trace.SpanFromContext(ctx)

	// Check if the span was sampled and is recording the data
	if !span.IsRecording() {
		ctx, span = s.tracer.Start(ctx, "ShortlinkController.HandleShortLink")
		defer span.End()
	}

	span.SetAttributes(
		attribute.String("shortlink", shortlinkName),
		attribute.String("referrer", ct.Request.Referer()),
	)

	ct.Header("Cache-Control", "public, max-age=900, stale-if-error=3600") // max-age = 15min; stale-if-error = 1h

	shortlink, err := s.client.Get(ctx, shortlinkName)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			otelzap.L().WithError(err).Ctx(ctx).Error("Path not found",
				zap.String("shortlink", shortlinkName),
				zap.String("operation", "shortlink"),
			)

			span.SetAttributes(attribute.String("path", ct.Request.URL.Path))

			ct.HTML(http.StatusNotFound, "404.html", gin.H{})
		} else {
			otelzap.L().WithError(err).Ctx(ctx).Error("Failed to get ShortLink",
				zap.String("shortlink", shortlinkName),
				zap.String("operation", "shortlink"),
			)

			ct.HTML(http.StatusInternalServerError, "500.html", gin.H{})
		}
		return
	}

	span.SetAttributes(
		attribute.String("Target", shortlink.Spec.Target),
		attribute.Int64("RedirectAfter", shortlink.Spec.RedirectAfter),
		attribute.Int("InvocationCount", shortlink.Status.Count),
	)

	target := shortlink.Spec.Target

	if !strings.HasPrefix(target, "http") {
		target = fmt.Sprintf("http://%s", target)

		span.AddEvent("change prefix", trace.WithAttributes(
			attribute.String("from", shortlink.Spec.Target),
			attribute.String("to", target),
		))
	}

	if shortlink.Spec.Code != 200 {
		// Redirect
		ct.Redirect(shortlink.Spec.Code, target)
	} else {
		// Redirect via JS/HTML
		ct.HTML(
			http.StatusOK,
			"redirect.html",
			gin.H{
				"redirectFrom":  ct.Request.URL.Path,
				"redirectTo":    target,
				"redirectAfter": shortlink.Spec.RedirectAfter,
			},
		)
	}

	// Increase hit counter
	s.client.IncrementInvocationCount(ct, shortlink)
}
