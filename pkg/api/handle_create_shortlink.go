package api

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/cedi/urlshortener/api/v1alpha1"
	"github.com/gin-gonic/gin"
	"github.com/sierrasoftworks/humane-errors-go"
	"github.com/spechtlabs/go-otel-utils/otelzap"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// HandleCreateShortLink handles the creation of a shortlink and redirects according to the configuration
// @BasePath /api/v1/
// @Summary       create new shortlink
// @Schemes       http https
// @Description   create a new shortlink
// @Accept        application/json
// @Produce       text/plain
// @Produce       application/json
// @Param         shortlink   path      string                 	false  					"the shortlink URL part (shortlink id)" example(home)
// @Param         spec        body      v1alpha1.ShortLinkSpec 	true   					"shortlink spec"
// @Success       200         {object}  int     				"Success"
// @Success       301         {object}  int     				"MovedPermanently"
// @Success       302         {object}  int     				"Found"
// @Success       307         {object}  int     				"TemporaryRedirect"
// @Success       308         {object}  int     				"PermanentRedirect"
// @Failure       401         {object}  int                     "Unauthorized"
// @Failure       404         {object}  int     				"NotFound"
// @Failure       500         {object}  int     				"InternalServerError"
// @Tags api/v1/
// @Router /api/v1/shortlink/{shortlink} [post]
// @Security bearerAuth
func (s *UrlshortenerServer) HandleCreateShortLink(ct *gin.Context) {
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
			zap.String("operation", "create"),
		)

		ct.JSON(http.StatusUnauthorized, gin.H{"error": err.Error(), "advice": err.Advice()})
		return
	}

	jsonData, err := io.ReadAll(ct.Request.Body)
	if err != nil {
		otelzap.L().WithError(err).Ctx(ctx).Error("Failed to read request-body",
			zap.String("shortlink", shortlinkName),
			zap.String("operation", "create"),
		)
		ct.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	shortlink := v1alpha1.ShortLink{
		ObjectMeta: v1.ObjectMeta{
			Name: shortlinkName,
		},
		Spec: v1alpha1.ShortLinkSpec{},
	}

	if err := json.Unmarshal(jsonData, &shortlink.Spec); err != nil {
		otelzap.L().WithError(err).Ctx(ctx).Error("Failed to read spec-json",
			zap.String("shortlink", shortlinkName),
			zap.String("operation", "create"),
		)
		ct.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if err := s.userClient.Create(ctx, userName, &shortlink); err != nil {
		otelzap.L().WithError(err).Ctx(ctx).Error("Failed to create ShortLink",
			zap.String("shortlink", shortlinkName),
			zap.String("operation", "create"),
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
