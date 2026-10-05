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

	span := trace.SpanFromContext(ct.Request.Context())
	span.SetAttributes(attribute.String("shortlink", shortlinkName), attribute.String("referrer", ct.Request.Referer()))

	if len(userName) == 0 {
		abortWithError(ct, http.StatusUnauthorized, operationCreate, newNoUserError())
		return
	}

	jsonData, err := io.ReadAll(ct.Request.Body)
	if err != nil {
		abortWithError(ct, http.StatusInternalServerError, operationCreate, humane.Wrap(err, "Failed to read request-body",
			"Send the ShortLink spec as the JSON body of the request",
		))
		return
	}

	shortlink := v1alpha1.Shortlink{
		Name: shortlinkName,
		Spec: v1alpha1.ShortlinkSpec{},
	}

	if err := json.Unmarshal(jsonData, &shortlink.Spec); err != nil {
		abortWithError(ct, http.StatusInternalServerError, operationCreate, humane.Wrap(err, "Failed to read spec-json",
			"Send the ShortLink spec as the JSON body of the request, e.g. {\"target\": \"https://example.com\"}",
		))
		return
	}

	if err := s.userClient.Create(ct.Request.Context(), userName, &shortlink); err != nil {
		abortWithError(ct, http.StatusInternalServerError, operationCreate, err)
		return
	}

	ct.JSON(http.StatusOK, v1alpha1.ShortLinkAPI{
		Name:   shortlink.Name,
		Spec:   shortlink.Spec,
		Status: shortlink.Status,
	})
}
