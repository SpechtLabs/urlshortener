package api

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/sierrasoftworks/humane-errors-go"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/spechtlabs/urlshortener/api/v1alpha1"
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
		abortWithError(ct, http.StatusUnauthorized, operationUpdate, newNoUserError())
		return
	}

	jsonData, err := io.ReadAll(ct.Request.Body)
	if err != nil {
		abortWithError(ct, http.StatusInternalServerError, operationUpdate, humane.Wrap(err, "Failed to read request-body",
			"Send the ShortLink spec as the JSON body of the request",
		))
		return
	}

	shortlinkSpec := v1alpha1.ShortlinkSpec{}
	if err := json.Unmarshal(jsonData, &shortlinkSpec); err != nil {
		abortWithError(ct, http.StatusInternalServerError, operationUpdate, humane.Wrap(err, "Failed to unmarshal ShortLink Spec JSON",
			"Send the ShortLink spec as the JSON body of the request, e.g. {\"target\": \"https://example.com\"}",
		))
		return
	}

	shortlink, herr := s.userClient.Get(ctx, userName, shortlinkName)
	if herr != nil {
		abortWithError(ct, statusFor(herr), operationUpdate, herr)
		return
	}

	shortlink.Spec = shortlinkSpec

	if err := s.userClient.Update(ctx, userName, shortlink); err != nil {
		abortWithError(ct, http.StatusInternalServerError, operationUpdate, err)
		return
	}

	ct.JSON(http.StatusOK, v1alpha1.ShortLinkAPI{
		Name:   shortlink.Name,
		Spec:   shortlink.Spec,
		Status: shortlink.Status,
	})
}
