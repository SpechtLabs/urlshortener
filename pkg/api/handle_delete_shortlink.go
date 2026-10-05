package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// HandleDeleteShortLink handles the deletion of a shortlink
// @BasePath /api/v1/
// @Summary       delete shortlink
// @Schemes       http https
// @Description   delete shortlink
// @Produce       text/plain
// @Produce       application/json
// @Param         shortlink   path      string                 true   "the shortlink URL part (shortlink id)" example(home)
// @Success       200         {object}  int     "Success"
// @Failure       401         {object}  int     "Unauthorized"
// @Failure       404         {object}  int     "NotFound"
// @Failure       500         {object}  int     "InternalServerError"
// @Tags api/v1/
// @Router /api/v1/shortlink/{shortlink} [delete]
// @Security bearerAuth
func (s *UrlshortenerServer) HandleDeleteShortLink(ct *gin.Context) {
	shortlinkName := ct.Param("shortlink")
	userName := ct.GetString("githubUserName")

	ctx := ct.Request.Context()
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.String("shortlink", shortlinkName), attribute.String("referrer", ct.Request.Referer()))

	if len(userName) == 0 {
		abortWithError(ct, http.StatusUnauthorized, operationDelete, newNoUserError())
		return
	}

	shortlink, err := s.userClient.Get(ctx, userName, shortlinkName)
	if err != nil {
		abortWithError(ct, statusFor(err), operationDelete, err)
		return
	}

	if err := s.userClient.Delete(ctx, userName, shortlink); err != nil {
		abortWithError(ct, statusFor(err), operationDelete, err)
		return
	}
}
