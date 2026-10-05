package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sierrasoftworks/humane-errors-go"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// gitHubUserURL is the GitHub API endpoint that describes the token's user.
const gitHubUserURL = "https://api.github.com/user"

// gitHubTimeout bounds a request to the GitHub API, so a slow GitHub can't
// hold API requests open.
const gitHubTimeout = 10 * time.Second

// headerAdvice tells a client how to authenticate.
const headerAdvice = "ensure you include a Bearer token in the Authorization header, e.g. Authorization: Bearer <token> or Authorization: token <token>"

// GithubUser is the part of GitHub's user object the API uses to identify the
// owner of a ShortLink.
type GithubUser struct {
	Login     string `json:"login,omitempty"`
	AvatarUrl string `json:"avatar_url,omitempty"`
	Type      string `json:"type,omitempty"`
	Name      string `json:"name,omitempty"`
	Email     string `json:"email,omitempty"`
	Id        int    `json:"id,omitempty"`
}

func extractBearerToken(c *gin.Context) (string, humane.Error) {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		return "", humane.New("Missing Authorization header", headerAdvice)
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		return "", humane.New("Invalid Authorization header format", headerAdvice)
	}

	return parts[1], nil
}

// getGitHubUserInfo asks userURL (gitHubUserURL outside of tests) whose token
// bearerToken is.
func getGitHubUserInfo(ctx context.Context, userURL, bearerToken string) (*GithubUser, humane.Error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, userURL, http.NoBody)
	if err != nil {
		return nil, humane.Wrap(err, "Failed to build request to fetch GitHub API", "This is a bug in the GitHub user lookup; please report it")
	}

	req.Header.Add("Accept", "application/vnd.github.v3+json")
	req.Header.Add("Authorization", "token "+bearerToken)

	client := &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
		Timeout:   gitHubTimeout,
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, humane.Wrap(err, "Failed to fetch UserInfo from GitHub API", "Check that the server can reach api.github.com, then try again")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, humane.New(fmt.Sprintf("bad credentials: GitHub answered %s", resp.Status), headerAdvice,
			"The token must be a valid GitHub token that may read the user's profile",
		)
	}

	githubUser := &GithubUser{}
	if err := json.NewDecoder(resp.Body).Decode(githubUser); err != nil {
		return nil, humane.Wrap(err, "Failed to unmarshal GitHub UserInfo", "GitHub answered with something other than a user object; try again later")
	}

	return githubUser, nil
}
