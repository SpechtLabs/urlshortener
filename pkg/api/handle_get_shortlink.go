package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sierrasoftworks/humane-errors-go"

	"github.com/spechtlabs/go-otel-utils/otelzap"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/spechtlabs/urlshortener/api/v1alpha1"
)

// HandleGetShortLink returns the shortlink
// @BasePath      /api/v1/
// @Summary       get a shortlink
// @Schemes       http https
// @Description   get a shortlink
// @Produce       text/plain
// @Produce       application/json
// @Param         shortlink   path      string    false          "the shortlink URL part (shortlink id)" example(home)
// @Success       200         {object}  ShortLink "Success"
// @Failure       401         {object}  int       "Unauthorized"
// @Failure       404         {object}  int       "NotFound"
// @Failure       500         {object}  int       "InternalServerError"
// @Tags api/v1/
// @Router /api/v1/shortlink/{shortlink} [get]
// @Security bearerAuth
func (s *UrlshortenerServer) HandleGetShortLink(ct *gin.Context) {
	shortlinkName := ct.Param("shortlink")
	userName := ct.GetString("githubUserName")

	ctx := ct.Request.Context()
	span := trace.SpanFromContext(ctx)

	span.SetAttributes(attribute.String("shortlink", shortlinkName), attribute.String("referrer", ct.Request.Referer()))

	if len(userName) == 0 {
		err := humane.New("No user found for request",
			"ensure you include a Bearer token in the Authorization header, e.g. Authorization: Bearer <token> or Authorization: token <token>",
		)

		otelzap.L().WithError(err).Ctx(ctx).Error(err.Error(),
			zap.String("shortlink", shortlinkName),
			zap.String("operation", "delete"),
		)

		ct.JSON(http.StatusUnauthorized, gin.H{"error": err.Error(), "advice": err.Advice()})
		return
	}

	shortlink, err := s.userClient.Get(ctx, userName, shortlinkName)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") {
			statusCode = http.StatusNotFound
		}

		otelzap.L().WithError(err).Ctx(ctx).Error("Failed to get ShortLink",
			zap.String("shortlink", shortlinkName),
			zap.String("operation", "get"),
		)

		ct.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}

	ct.JSON(http.StatusOK, v1alpha1.ShortLinkAPI{
		Name:   shortlink.Name,
		Spec:   shortlink.Spec,
		Status: shortlink.Status,
	})
}
