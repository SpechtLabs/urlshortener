package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/cedi/urlshortener/api/v1alpha1"
	"github.com/gin-gonic/gin"
	"github.com/sierrasoftworks/humane-errors-go"
	"github.com/spechtlabs/go-otel-utils/otelzap"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// HandleUpdateShortLink handles the update of a shortlink
// @BasePath /api/v1/
// @Summary       update existing shortlink
// @Schemes       http https
// @Description   update a new shortlink
// @Accept        application/json
// @Produce       text/plain
// @Produce       application/json
// @Param         shortlink   path      string                 true   "the shortlink URL part (shortlink id)" example(home)
// @Param         spec        body      v1alpha1.ShortLinkSpec true   "shortlink spec"
// @Success       200         {object}  int     "Success"
// @Failure       401         {object}  int     "Unauthorized"
// @Failure       404         {object}  int     "NotFound"
// @Failure       500         {object}  int     "InternalServerError"
// @Tags api/v1/
// @Router /api/v1/shortlink/{shortlink} [put]
// @Security bearerAuth
func (s *UrlshortenerServer) HandleUpdateShortLink(ct *gin.Context) {
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
			zap.String("operation", "list"),
		)

		ct.JSON(http.StatusUnauthorized, gin.H{"error": err.Error(), "advice": err.Advice()})
		return
	}

	jsonData, err := io.ReadAll(ct.Request.Body)
	if err != nil {
		herr := humane.Wrap(err, "Failed to read request-body")

		otelzap.L().WithError(err).Ctx(ctx).Error(herr.Error(),
			zap.String("shortlink", shortlinkName),
			zap.String("operation", "update"),
		)

		ct.JSON(http.StatusInternalServerError, gin.H{"error": herr.Error(), "cause": herr.Cause()})
		return
	}

	shortlinkSpec := v1alpha1.ShortLinkSpec{}
	if err := json.Unmarshal(jsonData, &shortlinkSpec); err != nil {
		herr := humane.Wrap(err, "Failed to unmarshal ShortLink Spec JSON")

		otelzap.L().WithError(herr).Ctx(ctx).Error(herr.Error(),
			zap.String("shortlink", shortlinkName),
			zap.String("operation", "update"),
		)

		ct.JSON(http.StatusInternalServerError, gin.H{"error": herr.Error(), "cause": herr.Cause()})
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
			zap.String("operation", "delete"),
		)

		ct.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}

	// When shortlink was not found
	if shortlink == nil {
		ct.JSON(http.StatusInternalServerError, gin.H{"error": "Shortlink not found"})
		return
	}
	shortlink.Spec = shortlinkSpec

	if err := s.userClient.Update(ctx, userName, shortlink); err != nil {
		otelzap.L().WithError(err).Ctx(ctx).Error("Failed to update ShortLink",
			zap.String("shortlink", shortlinkName),
			zap.String("operation", "update"),
		)

		ct.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ct.JSON(http.StatusOK, ShortLink{
		Name:   shortlink.Name,
		Spec:   shortlink.Spec,
		Status: shortlink.Status,
	})
}
