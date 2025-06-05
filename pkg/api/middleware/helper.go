package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/sierrasoftworks/humane-errors-go"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type GithubUser struct {
	Id        int    `json:"id,omitempty"`
	Login     string `json:"login,omitempty"`
	AvatarUrl string `json:"avatar_url,omitempty"`
	Type      string `json:"type,omitempty"`
	Name      string `json:"name,omitempty"`
	Email     string `json:"email,omitempty"`
}

func extractBearerToken(c *gin.Context) (string, humane.Error) {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		return "", humane.New("Missing Authorization header",
			"ensure you include a Bearer token in the Authorization header, e.g. Authorization: Bearer <token> or Authorization: token <token>",
		)
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		return "", humane.New("Invalid Authorization header format",
			"ensure you include a Bearer token in the Authorization header, e.g. Authorization: Bearer <token> or Authorization: token <token>",
		)
	}

	return parts[1], nil
}

func getGitHubUserInfo(c context.Context, bearerToken string) (*GithubUser, error) {
	// prepare request to GitHubs User endpoint
	req, err := http.NewRequestWithContext(c, http.MethodGet, "https://api.github.com/user", nil)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to build request to fetch GitHub API")
	}

	// Set headers
	req.Header.Add("Accept", "application/vnd.github.v3+json")
	req.Header.Add("Authorization", "token "+bearerToken)

	// Perform request
	client := &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to fetch UserInfo from GitHub API")
	}
	defer resp.Body.Close()

	// If request was unsuccessful, we error out
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("bad credentials")
	}

	// If successful, we read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "Error while reading the response")
	}

	// And parse it in our GithubUser model
	githubUser := &GithubUser{}
	err = json.Unmarshal(body, githubUser)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to unmarshal GitHub UserInfo")
	}

	return githubUser, nil
}
