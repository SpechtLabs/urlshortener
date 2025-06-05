package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/spechtlabs/go-otel-utils/otelzap"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

func GitHubUserAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		span := trace.SpanFromContext(ctx)

		shortlinkName := c.Param("shortlink")
		if len(shortlinkName) != 0 {
			span.SetAttributes(attribute.String("shortlink", shortlinkName))
		}

		span.SetAttributes(attribute.String("referrer", c.Request.Referer()))

		tokenString, err := extractBearerToken(c)
		if err != nil {
			otelzap.L().WithError(err).Ctx(ctx).Error(err.Error(),
				zap.String("shortlink", shortlinkName),
				zap.String("method", c.Request.Method),
			)

			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error(), "advice": err.Advice()})
			return
		}

		if user, err := getGitHubUserInfo(ctx, tokenString); err != nil {
			otelzap.L().WithError(err).Ctx(ctx).Error(err.Error(),
				zap.String("shortlink", shortlinkName),
				zap.String("method", c.Request.Method),
			)
		} else {
			c.Set("githubUserName", user.Name)
		}

		c.Next()
	}
}
