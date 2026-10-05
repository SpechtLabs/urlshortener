package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

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
		abortWithError(ct, http.StatusUnauthorized, operationGet, newNoUserError())
		return
	}

	shortlink, err := s.userClient.Get(ctx, userName, shortlinkName)
	if err != nil {
		abortWithError(ct, statusFor(err), operationGet, err)
		return
	}

	ct.JSON(http.StatusOK, v1alpha1.ShortLinkAPI{
		Name:   shortlink.Name,
		Spec:   shortlink.Spec,
		Status: shortlink.Status,
	})
}
