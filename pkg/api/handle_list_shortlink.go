package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/spechtlabs/urlshortener/api/v1alpha1"
)

// HandleListShortLink handles the listing of
// @BasePath /api/v1/
// @Summary       list shortlinks
// @Schemes       http https
// @Description   list shortlinks
// @Produce       text/plain
// @Produce       application/json
// @Success       200         {object} []ShortLink "Success"
// @Failure       401         {object} int         "Unauthorized"
// @Failure       404         {object} int         "NotFound"
// @Failure       500         {object} int         "InternalServerError"
// @Tags api/v1/
// @Router /api/v1/shortlink/ [get]
// @Security bearerAuth
func (s *UrlshortenerServer) HandleListShortLink(ct *gin.Context) {
	userName := ct.GetString("githubUserName")

	ctx := ct.Request.Context()
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.String("referrer", ct.Request.Referer()))

	if len(userName) == 0 {
		abortWithError(ct, http.StatusUnauthorized, operationList, newNoUserError())
		return
	}

	shortlinkList, err := s.userClient.List(ctx, userName)
	if err != nil {
		abortWithError(ct, statusFor(err), operationList, err)
		return
	}

	targetList := make([]v1alpha1.ShortLinkAPI, len(shortlinkList.Items))

	for idx, shortlink := range shortlinkList.Items {
		targetList[idx] = v1alpha1.ShortLinkAPI{
			Name:   shortlink.Name,
			Spec:   shortlink.Spec,
			Status: shortlink.Status,
		}
	}

	ct.JSON(http.StatusOK, targetList)
}
