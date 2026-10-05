package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestGitHubUserAuth(t *testing.T) {
	tests := []struct {
		name          string
		authorization string
		github        http.HandlerFunc
		unreachable   bool
		wantStatus    int
		wantUser      string
	}{
		{
			name:          "a valid token",
			authorization: "Bearer gho_valid",
			github:        githubUser(t, `{"login": "octocat", "name": "The Octocat"}`),
			wantStatus:    http.StatusOK,
			wantUser:      "The Octocat",
		},
		{
			name:          "the scheme in lower case",
			authorization: "bearer gho_valid",
			github:        githubUser(t, `{"login": "octocat", "name": "The Octocat"}`),
			wantStatus:    http.StatusOK,
			wantUser:      "The Octocat",
		},
		{
			name:       "no Authorization header",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:          "not a bearer token",
			authorization: "Basic b2N0b2NhdDpzZWNyZXQ=",
			wantStatus:    http.StatusUnauthorized,
		},
		{
			name:          "a token GitHub rejects",
			authorization: "Bearer gho_revoked",
			github:        githubUser(t, `{}`),
			wantStatus:    http.StatusUnauthorized,
		},
		{
			name:          "GitHub answers something other than a user",
			authorization: "Bearer gho_valid",
			github:        githubUser(t, `not json`),
			wantStatus:    http.StatusUnauthorized,
		},
		{
			name:          "GitHub is unreachable",
			authorization: "Bearer gho_valid",
			unreachable:   true,
			wantStatus:    http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userURL := "http://127.0.0.1:0/user"
			if !tt.unreachable && tt.github != nil {
				github := httptest.NewServer(tt.github)
				defer github.Close()

				userURL = github.URL + "/user"
			}

			var gotUser string

			gin.SetMode(gin.TestMode)
			router := gin.New()
			router.GET("/api", gitHubUserAuth(userURL), func(c *gin.Context) {
				gotUser = c.GetString("githubUserName")
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api", http.NoBody)
			if tt.authorization != "" {
				req.Header.Set("Authorization", tt.authorization)
			}

			resp := httptest.NewRecorder()
			router.ServeHTTP(resp, req)

			if resp.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", resp.Code, tt.wantStatus, resp.Body.String())
			}

			if gotUser != tt.wantUser {
				t.Errorf("githubUserName = %q, want %q", gotUser, tt.wantUser)
			}

			if tt.wantStatus == http.StatusUnauthorized {
				var body struct {
					Error  string   `json:"error"`
					Advice []string `json:"advice"`
				}

				if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil || body.Error == "" || len(body.Advice) == 0 {
					t.Errorf("401 body lacks the error or its advice: %s", resp.Body.String())
				}
			}
		})
	}
}

func TestGitHubUserAuthMiddleware(t *testing.T) {
	// Without a token the middleware answers before it would ask GitHub.
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api", GitHubUserAuthMiddleware(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api", http.NoBody))

	if resp.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.Code, http.StatusUnauthorized)
	}
}

// githubUser answers GitHub's user endpoint with body for the token
// gho_valid, and with 401 for any other token.
func githubUser(t *testing.T, body string) http.HandlerFunc {
	t.Helper()

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token gho_valid" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		if _, err := w.Write([]byte(body)); err != nil {
			t.Error(err)
		}
	}
}
