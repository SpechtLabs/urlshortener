package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/spechtlabs/urlshortener/api/v1alpha1"
	shortlinkClient "github.com/spechtlabs/urlshortener/pkg/client"
)

// testNamespace is the namespace the test server works in.
const testNamespace = "urlshortener"

// userHeader names the GitHub user of a test request; the test router sets
// it where the auth middleware would.
const userHeader = "X-Test-User"

var errInjected = errors.New("injected")

func TestHandleShortLink(t *testing.T) {
	tests := []struct {
		name         string
		shortlink    *v1alpha1.Shortlink
		intercept    *interceptor.Funcs
		outsidePod   bool
		path         string
		wantStatus   int
		wantLocation string
		wantBody     string
		wantCount    int
	}{
		{
			name:         "redirects with the shortlink's code",
			shortlink:    newShortlink("home", "octocat", "https://example.com", 301),
			path:         "/home",
			wantStatus:   http.StatusMovedPermanently,
			wantLocation: "https://example.com",
			wantCount:    1,
		},
		{
			name:         "prepends http:// to a target without a protocol",
			shortlink:    newShortlink("home", "octocat", "example.com", 307),
			path:         "/home",
			wantStatus:   http.StatusTemporaryRedirect,
			wantLocation: "http://example.com",
			wantCount:    1,
		},
		{
			name:       "redirects from an HTML page for code 200",
			shortlink:  newShortlink("home", "octocat", "https://example.com", 200),
			path:       "/home",
			wantStatus: http.StatusOK,
			wantBody:   "https://example.com",
			wantCount:  1,
		},
		{
			name:         "still redirects when counting fails",
			shortlink:    newShortlink("home", "octocat", "https://example.com", 302),
			intercept:    &interceptor.Funcs{SubResourceUpdate: failSubResourceUpdate},
			path:         "/home",
			wantStatus:   http.StatusFound,
			wantLocation: "https://example.com",
		},
		{
			name:       "answers 404 for a missing shortlink",
			path:       "/missing",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "answers 500 when the shortlink can't be read",
			outsidePod: true,
			path:       "/home",
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var objects []client.Object
			if tt.shortlink != nil {
				objects = append(objects, tt.shortlink)
			}

			k8s, router := newTestRouter(t, tt.outsidePod, tt.intercept, objects...)

			resp := serve(router, http.MethodGet, tt.path, "", nil)
			if resp.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", resp.Code, tt.wantStatus)
			}

			if got := resp.Header().Get("Location"); got != tt.wantLocation {
				t.Errorf("Location = %q, want %q", got, tt.wantLocation)
			}

			if !strings.Contains(resp.Body.String(), tt.wantBody) {
				t.Errorf("body doesn't contain %q:\n%s", tt.wantBody, resp.Body.String())
			}

			if tt.shortlink != nil {
				if got := getShortlink(t, k8s, tt.shortlink.Name); got.Status.Count != tt.wantCount {
					t.Errorf("invocation count = %d, want %d", got.Status.Count, tt.wantCount)
				}
			}
		})
	}
}

func TestHandleListShortLink(t *testing.T) {
	tests := []struct {
		name       string
		user       string
		wantStatus int
		wantNames  []string
	}{
		{name: "lists the user's shortlinks", user: "octocat", wantStatus: http.StatusOK, wantNames: []string{"home"}},
		{name: "without a user", wantStatus: http.StatusUnauthorized},
		{name: "a user who owns none", user: "nobody", wantStatus: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, router := newTestRouter(t, false, nil,
				newShortlink("home", "octocat", "https://example.com", 307),
				newShortlink("blog", "hubot", "https://example.org", 307),
			)

			resp := serve(router, http.MethodGet, "/api/v1/shortlink/", tt.user, nil)
			if resp.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", resp.Code, tt.wantStatus, resp.Body.String())
			}

			if tt.wantStatus != http.StatusOK {
				checkErrorBody(t, resp)
				return
			}

			var got []v1alpha1.ShortLinkAPI
			if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}

			if len(got) != len(tt.wantNames) || got[0].Name != tt.wantNames[0] {
				t.Errorf("listed %+v, want %v", got, tt.wantNames)
			}
		})
	}
}

func TestHandleGetShortLink(t *testing.T) {
	tests := []struct {
		name       string
		user       string
		path       string
		wantStatus int
	}{
		{name: "the owner's shortlink", user: "octocat", path: "/api/v1/shortlink/home", wantStatus: http.StatusOK},
		{name: "without a user", path: "/api/v1/shortlink/home", wantStatus: http.StatusUnauthorized},
		{name: "someone else's shortlink", user: "hubot", path: "/api/v1/shortlink/home", wantStatus: http.StatusInternalServerError},
		{name: "a missing shortlink", user: "octocat", path: "/api/v1/shortlink/missing", wantStatus: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, router := newTestRouter(t, false, nil, newShortlink("home", "octocat", "https://example.com", 307))

			resp := serve(router, http.MethodGet, tt.path, tt.user, nil)
			if resp.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", resp.Code, tt.wantStatus, resp.Body.String())
			}

			if tt.wantStatus != http.StatusOK {
				checkErrorBody(t, resp)
				return
			}

			var got v1alpha1.ShortLinkAPI
			if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}

			if got.Name != "home" || got.Spec.Target != "https://example.com" {
				t.Errorf("got %+v", got)
			}
		})
	}
}

func TestHandleCreateShortLink(t *testing.T) {
	tests := []struct {
		name       string
		user       string
		body       io.Reader
		existing   []client.Object
		wantStatus int
	}{
		{name: "creates the shortlink", user: "octocat", body: strings.NewReader(`{"target": "https://example.com"}`), wantStatus: http.StatusOK},
		{name: "without a user", body: strings.NewReader(`{}`), wantStatus: http.StatusUnauthorized},
		{name: "an unreadable body", user: "octocat", body: errReader{}, wantStatus: http.StatusInternalServerError},
		{name: "a body that isn't JSON", user: "octocat", body: strings.NewReader(`target`), wantStatus: http.StatusInternalServerError},
		{
			name:       "a shortlink that exists",
			user:       "octocat",
			body:       strings.NewReader(`{"target": "https://example.com"}`),
			existing:   []client.Object{newShortlink("home", "hubot", "https://example.org", 307)},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k8s, router := newTestRouter(t, false, nil, tt.existing...)

			resp := serve(router, http.MethodPost, "/api/v1/shortlink/home", tt.user, tt.body)
			if resp.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", resp.Code, tt.wantStatus, resp.Body.String())
			}

			if tt.wantStatus != http.StatusOK {
				checkErrorBody(t, resp)
				return
			}

			if got := getShortlink(t, k8s, "home"); got.Spec.Owner != "octocat" || got.Spec.Target != "https://example.com" {
				t.Errorf("created %+v", got.Spec)
			}
		})
	}
}

func TestHandleUpdateShortLink(t *testing.T) {
	tests := []struct {
		name       string
		user       string
		path       string
		body       io.Reader
		intercept  *interceptor.Funcs
		wantStatus int
	}{
		{name: "updates the shortlink", user: "octocat", path: "/api/v1/shortlink/home", body: strings.NewReader(`{"owner": "octocat", "target": "https://example.org"}`), wantStatus: http.StatusOK},
		{name: "without a user", path: "/api/v1/shortlink/home", body: strings.NewReader(`{}`), wantStatus: http.StatusUnauthorized},
		{name: "an unreadable body", user: "octocat", path: "/api/v1/shortlink/home", body: errReader{}, wantStatus: http.StatusInternalServerError},
		{name: "a body that isn't JSON", user: "octocat", path: "/api/v1/shortlink/home", body: strings.NewReader(`target`), wantStatus: http.StatusInternalServerError},
		{name: "a missing shortlink", user: "octocat", path: "/api/v1/shortlink/missing", body: strings.NewReader(`{}`), wantStatus: http.StatusNotFound},
		{name: "someone else's shortlink", user: "hubot", path: "/api/v1/shortlink/home", body: strings.NewReader(`{}`), wantStatus: http.StatusInternalServerError},
		{
			name:       "the update fails",
			user:       "octocat",
			path:       "/api/v1/shortlink/home",
			body:       strings.NewReader(`{"owner": "octocat", "target": "https://example.org"}`),
			intercept:  &interceptor.Funcs{Update: failUpdate},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k8s, router := newTestRouter(t, false, tt.intercept, newShortlink("home", "octocat", "https://example.com", 307))

			resp := serve(router, http.MethodPut, tt.path, tt.user, tt.body)
			if resp.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", resp.Code, tt.wantStatus, resp.Body.String())
			}

			if tt.wantStatus != http.StatusOK {
				checkErrorBody(t, resp)
				return
			}

			got := getShortlink(t, k8s, "home")
			if got.Spec.Target != "https://example.org" || got.Status.ChangedBy != "octocat" {
				t.Errorf("after the update: target %q, changedBy %q", got.Spec.Target, got.Status.ChangedBy)
			}
		})
	}
}

func TestHandleDeleteShortLink(t *testing.T) {
	tests := []struct {
		name       string
		user       string
		path       string
		intercept  *interceptor.Funcs
		wantStatus int
	}{
		{name: "deletes the shortlink", user: "octocat", path: "/api/v1/shortlink/home", wantStatus: http.StatusOK},
		{name: "without a user", path: "/api/v1/shortlink/home", wantStatus: http.StatusUnauthorized},
		{name: "a missing shortlink", user: "octocat", path: "/api/v1/shortlink/missing", wantStatus: http.StatusNotFound},
		{name: "someone else's shortlink", user: "hubot", path: "/api/v1/shortlink/home", wantStatus: http.StatusInternalServerError},
		{name: "the delete fails", user: "octocat", path: "/api/v1/shortlink/home", intercept: &interceptor.Funcs{Delete: failDelete}, wantStatus: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, router := newTestRouter(t, false, tt.intercept, newShortlink("home", "octocat", "https://example.com", 307))

			resp := serve(router, http.MethodDelete, tt.path, tt.user, nil)
			if resp.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", resp.Code, tt.wantStatus, resp.Body.String())
			}

			if tt.wantStatus != http.StatusOK {
				checkErrorBody(t, resp)
			}
		})
	}
}

// newTestRouter returns a fake cluster holding objects and a router serving
// the handlers from it. The router takes the GitHub user from userHeader
// rather than from GitHub. Outside a pod, the server can't tell its
// namespace.
func newTestRouter(t *testing.T, outsidePod bool, intercept *interceptor.Funcs, objects ...client.Object) (client.Client, *gin.Engine) {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	builder := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		WithStatusSubresource(&v1alpha1.Shortlink{})

	if intercept != nil {
		builder = builder.WithInterceptorFuncs(*intercept)
	}

	k8s := builder.Build()

	namespaceFile := filepath.Join(t.TempDir(), "namespace")
	if !outsidePod {
		if err := os.WriteFile(namespaceFile, []byte(testNamespace), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	sClient := shortlinkClient.NewShortlinkClient(k8s, shortlinkClient.WithNamespaceFile(namespaceFile))
	s := &UrlshortenerServer{
		tracer:     otel.Tracer("urlshortener-test"),
		client:     sClient,
		userClient: shortlinkClient.NewUserShortLinkClient(sClient),
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.LoadHTMLGlob("../../html/templates/*.html")

	router.GET("/:shortlink", s.HandleShortLink)

	v1 := router.Group("/api/v1", func(c *gin.Context) {
		if user := c.GetHeader(userHeader); user != "" {
			c.Set("githubUserName", user)
		}
	})
	v1.GET("/shortlink/", s.HandleListShortLink)
	v1.GET("/shortlink/:shortlink", s.HandleGetShortLink)
	v1.POST("/shortlink/:shortlink", s.HandleCreateShortLink)
	v1.PUT("/shortlink/:shortlink", s.HandleUpdateShortLink)
	v1.DELETE("/shortlink/:shortlink", s.HandleDeleteShortLink)

	return k8s, router
}

func serve(router http.Handler, method, path, user string, body io.Reader) *httptest.ResponseRecorder {
	if body == nil {
		body = http.NoBody
	}

	req := httptest.NewRequestWithContext(context.Background(), method, path, body)
	if user != "" {
		req.Header.Set(userHeader, user)
	}

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	return resp
}

// checkErrorBody fails t unless resp's body is an error with advice.
func checkErrorBody(t *testing.T, resp *httptest.ResponseRecorder) {
	t.Helper()

	var body struct {
		Error  string   `json:"error"`
		Advice []string `json:"advice"`
	}

	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("error body isn't JSON: %v\n%s", err, resp.Body.String())
	}

	if body.Error == "" || len(body.Advice) == 0 {
		t.Errorf("error body lacks the error or its advice: %s", resp.Body.String())
	}
}

func newShortlink(name, owner, target string, code int) *v1alpha1.Shortlink {
	return &v1alpha1.Shortlink{
		Name: name, Namespace: testNamespace,
		Spec: v1alpha1.ShortlinkSpec{Owner: owner, Target: target, Code: code},
	}
}

func getShortlink(t *testing.T, k8s client.Client, name string) *v1alpha1.Shortlink {
	t.Helper()

	shortlink := &v1alpha1.Shortlink{}
	if err := k8s.Get(context.Background(), types.NamespacedName{Name: name, Namespace: testNamespace}, shortlink); err != nil {
		t.Fatal(err)
	}

	return shortlink
}

// errReader is a request body that can't be read.
type errReader struct{}

func (errReader) Read([]byte) (int, error) {
	return 0, errInjected
}

func failUpdate(context.Context, client.WithWatch, client.Object, ...client.UpdateOption) error {
	return errInjected
}

func failDelete(context.Context, client.WithWatch, client.Object, ...client.DeleteOption) error {
	return errInjected
}

func failSubResourceUpdate(context.Context, client.Client, string, client.Object, ...client.SubResourceUpdateOption) error {
	return errInjected
}
