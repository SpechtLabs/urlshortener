package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/sierrasoftworks/humane-errors-go"
	"github.com/spechtlabs/go-otel-utils/otelzap"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// GitHubUserAuthMiddleware identifies the GitHub user whose token the request
// carries, and stores their name as githubUserName in the gin context. A
// request without a valid token is answered with 401.
func GitHubUserAuthMiddleware() gin.HandlerFunc {
	return gitHubUserAuth(gitHubUserURL)
}

func gitHubUserAuth(userURL string) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		span := trace.SpanFromContext(ctx)

		shortlinkName := c.Param("shortlink")
		if len(shortlinkName) != 0 {
			span.SetAttributes(attribute.String("shortlink", shortlinkName))
		}

		span.SetAttributes(attribute.String("referrer", c.Request.Referer()))

		user, err := authenticate(c, userURL)
		if err != nil {
			otelzap.L().WithError(err).ErrorContext(ctx, err.Error(),
				zap.String("shortlink", shortlinkName),
				zap.String("method", c.Request.Method),
			)

			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error(), "advice": err.Advice()})
			return
		}

		c.Set("githubUserName", user.Name)
		c.Next()
	}
}

// authenticate returns the GitHub user whose token the request carries.
func authenticate(c *gin.Context, userURL string) (*GithubUser, humane.Error) {
	tokenString, err := extractBearerToken(c)
	if err != nil {
		return nil, err
	}

	return getGitHubUserInfo(c.Request.Context(), userURL, tokenString)
}
