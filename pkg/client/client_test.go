package client

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/spechtlabs/urlshortener/api/v1alpha1"
)

// testNamespace is the namespace the namespace file written by
// namespaceFile names.
const testNamespace = "urlshortener"

var errInjected = errors.New("injected")

func TestCurrentNamespace(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		want    string
		wantErr bool
	}{
		{name: "reads the namespace file", file: namespaceFile(t), want: testNamespace},
		{name: "fails without the namespace file", file: filepath.Join(t.TempDir(), "missing"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := currentNamespace(tt.file)
			if (err != nil) != tt.wantErr {
				t.Fatalf("currentNamespace() error = %v, wantErr %v", err, tt.wantErr)
			}

			if err != nil && len(err.Advice()) == 0 {
				t.Errorf("currentNamespace() error has no advice")
			}

			if got != tt.want {
				t.Errorf("currentNamespace() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewNotAllowedError(t *testing.T) {
	err := NewNotAllowedError("octocat", DeleteOperation, "home")

	want := "Operation 'delete' for user 'octocat' is not allowed for ShortLink 'home'"
	if err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}

	if len(err.Advice()) == 0 {
		t.Errorf("Advice() is empty")
	}
}

func TestShortlinkClientGet(t *testing.T) {
	tests := []struct {
		name         string
		file         string
		objects      []client.Object
		wantErr      bool
		wantNotFound bool
	}{
		{name: "found", file: namespaceFile(t), objects: []client.Object{newShortlink("home", "octocat")}},
		{name: "not found", file: namespaceFile(t), wantErr: true, wantNotFound: true},
		{name: "outside a pod", file: filepath.Join(t.TempDir(), "missing"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewShortlinkClient(newFakeClient(t, nil, tt.objects...), WithNamespaceFile(tt.file))

			got, err := c.Get(context.Background(), "home")
			checkErr(t, err, tt.wantErr, tt.wantNotFound)

			if !tt.wantErr && got.Spec.Target != "https://example.com" {
				t.Errorf("Get() returned target %q", got.Spec.Target)
			}
		})
	}
}

func TestShortlinkClientGetNameNamespace(t *testing.T) {
	c := NewShortlinkClient(newFakeClient(t, nil, newShortlink("home", "octocat")))

	got, err := c.GetNameNamespace(context.Background(), "home", testNamespace)
	if err != nil {
		t.Fatalf("GetNameNamespace() error = %v", err)
	}

	if got.Name != "home" {
		t.Errorf("GetNameNamespace() returned %q", got.Name)
	}
}

func TestShortlinkClientList(t *testing.T) {
	tests := []struct {
		name      string
		file      string
		intercept *interceptor.Funcs
		wantItems int
		wantErr   bool
	}{
		{name: "lists the namespace", file: namespaceFile(t), wantItems: 2},
		{name: "outside a pod", file: filepath.Join(t.TempDir(), "missing"), wantErr: true},
		{name: "list fails", file: namespaceFile(t), intercept: &interceptor.Funcs{List: failList}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k8s := newFakeClient(t, tt.intercept,
				newShortlink("home", "octocat"),
				newShortlink("blog", "hubot"),
				&v1alpha1.Shortlink{
					Name: "elsewhere", Namespace: "other",
					Spec: v1alpha1.ShortlinkSpec{Owner: "octocat", Target: "https://example.org"},
				},
			)
			c := NewShortlinkClient(k8s, WithNamespaceFile(tt.file))

			got, err := c.List(context.Background())
			checkErr(t, err, tt.wantErr, false)

			if !tt.wantErr && len(got.Items) != tt.wantItems {
				t.Errorf("List() returned %d items, want %d", len(got.Items), tt.wantItems)
			}
		})
	}
}

func TestShortlinkClientWrites(t *testing.T) {
	tests := []struct {
		name      string
		intercept *interceptor.Funcs
		write     func(context.Context, *ShortlinkClient, *v1alpha1.Shortlink) error
		check     func(*testing.T, client.Client)
		wantErr   bool
	}{
		{
			name: "update",
			write: func(ctx context.Context, c *ShortlinkClient, s *v1alpha1.Shortlink) error {
				s.Spec.Target = "https://example.org"
				return c.Update(ctx, s)
			},
			check: func(t *testing.T, k8s client.Client) {
				if got := getShortlink(t, k8s); got.Spec.Target != "https://example.org" {
					t.Errorf("target = %q after Update()", got.Spec.Target)
				}
			},
		},
		{
			name:      "update fails",
			intercept: &interceptor.Funcs{Update: failUpdate},
			write: func(ctx context.Context, c *ShortlinkClient, s *v1alpha1.Shortlink) error {
				return c.Update(ctx, s)
			},
			wantErr: true,
		},
		{
			name: "update status",
			write: func(ctx context.Context, c *ShortlinkClient, s *v1alpha1.Shortlink) error {
				s.Status.ChangedBy = "hubot"
				return c.UpdateStatus(ctx, s)
			},
			check: func(t *testing.T, k8s client.Client) {
				if got := getShortlink(t, k8s); got.Status.ChangedBy != "hubot" {
					t.Errorf("changedBy = %q after UpdateStatus()", got.Status.ChangedBy)
				}
			},
		},
		{
			name:      "update status fails",
			intercept: &interceptor.Funcs{SubResourceUpdate: failSubResourceUpdate},
			write: func(ctx context.Context, c *ShortlinkClient, s *v1alpha1.Shortlink) error {
				return c.UpdateStatus(ctx, s)
			},
			wantErr: true,
		},
		{
			name: "increment invocation count",
			write: func(ctx context.Context, c *ShortlinkClient, s *v1alpha1.Shortlink) error {
				return c.IncrementInvocationCount(ctx, s)
			},
			check: func(t *testing.T, k8s client.Client) {
				if got := getShortlink(t, k8s); got.Status.Count != 42 {
					t.Errorf("count = %d after IncrementInvocationCount(), want 42", got.Status.Count)
				}
			},
		},
		{
			name:      "increment invocation count fails",
			intercept: &interceptor.Funcs{SubResourceUpdate: failSubResourceUpdate},
			write: func(ctx context.Context, c *ShortlinkClient, s *v1alpha1.Shortlink) error {
				return c.IncrementInvocationCount(ctx, s)
			},
			wantErr: true,
		},
		{
			name: "delete",
			write: func(ctx context.Context, c *ShortlinkClient, s *v1alpha1.Shortlink) error {
				return c.Delete(ctx, s)
			},
			check: func(t *testing.T, k8s client.Client) {
				err := k8s.Get(context.Background(), types.NamespacedName{Name: "home", Namespace: testNamespace}, &v1alpha1.Shortlink{})
				if !k8serrors.IsNotFound(err) {
					t.Errorf("Get() after Delete() error = %v, want not found", err)
				}
			},
		},
		{
			name:      "delete fails",
			intercept: &interceptor.Funcs{Delete: failDelete},
			write: func(ctx context.Context, c *ShortlinkClient, s *v1alpha1.Shortlink) error {
				return c.Delete(ctx, s)
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k8s := newFakeClient(t, tt.intercept, newShortlink("home", "octocat"))
			c := NewShortlinkClient(k8s)

			err := tt.write(context.Background(), c, getShortlink(t, k8s))
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.check != nil {
				tt.check(t, k8s)
			}
		})
	}
}

func TestShortlinkClientCreate(t *testing.T) {
	tests := []struct {
		name          string
		file          string
		namespace     string
		existing      []client.Object
		wantNamespace string
		wantErr       bool
	}{
		{name: "in the current namespace", file: namespaceFile(t), wantNamespace: testNamespace},
		{name: "in the namespace it names", file: filepath.Join(t.TempDir(), "missing"), namespace: "other", wantNamespace: "other"},
		{name: "outside a pod", file: filepath.Join(t.TempDir(), "missing"), wantErr: true},
		{name: "already exists", file: namespaceFile(t), existing: []client.Object{newShortlink("home", "hubot")}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k8s := newFakeClient(t, nil, tt.existing...)
			c := NewShortlinkClient(k8s, WithNamespaceFile(tt.file))

			shortlink := &v1alpha1.Shortlink{
				Name: "home", Namespace: tt.namespace,
				Spec: v1alpha1.ShortlinkSpec{Owner: "octocat", Target: "https://example.com"},
			}

			err := c.Create(context.Background(), shortlink)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Create() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr {
				return
			}

			got := &v1alpha1.Shortlink{}
			if err := k8s.Get(context.Background(), types.NamespacedName{Name: "home", Namespace: tt.wantNamespace}, got); err != nil {
				t.Errorf("Get() after Create() error = %v", err)
			}
		})
	}
}

func TestRedirectClientReads(t *testing.T) {
	tests := []struct {
		name         string
		file         string
		intercept    *interceptor.Funcs
		read         func(context.Context, *RedirectClient) (int, error)
		want         int
		wantErr      bool
		wantNotFound bool
	}{
		{
			name: "get",
			file: namespaceFile(t),
			read: func(ctx context.Context, c *RedirectClient) (int, error) {
				_, err := c.Get(ctx, "cedi")
				return 1, err
			},
			want: 1,
		},
		{
			name: "get outside a pod",
			file: filepath.Join(t.TempDir(), "missing"),
			read: func(ctx context.Context, c *RedirectClient) (int, error) {
				_, err := c.Get(ctx, "cedi")
				return 0, err
			},
			wantErr: true,
		},
		{
			name: "get by name and namespace",
			read: func(ctx context.Context, c *RedirectClient) (int, error) {
				_, err := c.GetNameNamespace(ctx, "cedi", testNamespace)
				return 1, err
			},
			want: 1,
		},
		{
			name: "get a missing redirect",
			read: func(ctx context.Context, c *RedirectClient) (int, error) {
				_, err := c.GetNameNamespace(ctx, "missing", testNamespace)
				return 0, err
			},
			wantErr:      true,
			wantNotFound: true,
		},
		{
			name: "list all",
			read: func(ctx context.Context, c *RedirectClient) (int, error) {
				list, err := c.ListAll(ctx)
				return lenRedirects(list), err
			},
			want: 3,
		},
		{
			name:      "list all fails",
			intercept: &interceptor.Funcs{List: failList},
			read: func(ctx context.Context, c *RedirectClient) (int, error) {
				list, err := c.ListAll(ctx)
				return lenRedirects(list), err
			},
			wantErr: true,
		},
		{
			name: "list the current namespace",
			file: namespaceFile(t),
			read: func(ctx context.Context, c *RedirectClient) (int, error) {
				list, err := c.List(ctx)
				return lenRedirects(list), err
			},
			want: 2,
		},
		{
			name: "list outside a pod",
			file: filepath.Join(t.TempDir(), "missing"),
			read: func(ctx context.Context, c *RedirectClient) (int, error) {
				list, err := c.List(ctx)
				return lenRedirects(list), err
			},
			wantErr: true,
		},
		{
			name:      "list a namespace fails",
			intercept: &interceptor.Funcs{List: failList},
			read: func(ctx context.Context, c *RedirectClient) (int, error) {
				list, err := c.ListNamespaced(ctx, testNamespace)
				return lenRedirects(list), err
			},
			wantErr: true,
		},
		{
			name: "query by label",
			read: func(ctx context.Context, c *RedirectClient) (int, error) {
				list, err := c.Query(ctx, "blog")
				return lenRedirects(list), err
			},
			want: 1,
		},
		{
			name: "query with an invalid label",
			read: func(ctx context.Context, c *RedirectClient) (int, error) {
				list, err := c.Query(ctx, "not a label value")
				return lenRedirects(list), err
			},
			wantErr: true,
		},
		{
			name:      "query fails",
			intercept: &interceptor.Funcs{List: failList},
			read: func(ctx context.Context, c *RedirectClient) (int, error) {
				list, err := c.Query(ctx, "blog")
				return lenRedirects(list), err
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blog := newRedirect("blog", testNamespace)
			blog.Labels = map[string]string{"Redirect": "blog"}

			k8s := newFakeClient(t, tt.intercept, newRedirect("cedi", testNamespace), blog, newRedirect("cedi", "other"))
			c := NewRedirectClient(k8s, WithNamespaceFile(tt.file))

			got, err := tt.read(context.Background(), c)
			checkErr(t, err, tt.wantErr, tt.wantNotFound)

			if !tt.wantErr && got != tt.want {
				t.Errorf("got %d redirects, want %d", got, tt.want)
			}
		})
	}
}

func TestRedirectClientWrites(t *testing.T) {
	tests := []struct {
		name      string
		intercept *interceptor.Funcs
		write     func(context.Context, *RedirectClient, *v1alpha1.Redirect) error
		check     func(*v1alpha1.Redirect) bool
		wantErr   bool
	}{
		{
			name: "save",
			write: func(ctx context.Context, c *RedirectClient, r *v1alpha1.Redirect) error {
				r.Spec.Target = "https://example.org"
				return c.Save(ctx, r)
			},
			check: func(r *v1alpha1.Redirect) bool { return r.Spec.Target == "https://example.org" },
		},
		{
			name:      "save fails",
			intercept: &interceptor.Funcs{Update: failUpdate},
			write: func(ctx context.Context, c *RedirectClient, r *v1alpha1.Redirect) error {
				return c.Save(ctx, r)
			},
			wantErr: true,
		},
		{
			name: "save status",
			write: func(ctx context.Context, c *RedirectClient, r *v1alpha1.Redirect) error {
				r.Status.Target = "https://example.org"
				return c.SaveStatus(ctx, r)
			},
			check: func(r *v1alpha1.Redirect) bool { return r.Status.Target == "https://example.org" },
		},
		{
			name:      "save status fails",
			intercept: &interceptor.Funcs{SubResourceUpdate: failSubResourceUpdate},
			write: func(ctx context.Context, c *RedirectClient, r *v1alpha1.Redirect) error {
				return c.SaveStatus(ctx, r)
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k8s := newFakeClient(t, tt.intercept, newRedirect("cedi", testNamespace))
			c := NewRedirectClient(k8s)

			redirect, err := c.GetNameNamespace(context.Background(), "cedi", testNamespace)
			if err != nil {
				t.Fatalf("GetNameNamespace() error = %v", err)
			}

			if err := tt.write(context.Background(), c, redirect); (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.check == nil {
				return
			}

			got, err := c.GetNameNamespace(context.Background(), "cedi", testNamespace)
			if err != nil {
				t.Fatalf("GetNameNamespace() error = %v", err)
			}

			if !tt.check(got) {
				t.Errorf("the write didn't reach the cluster: %+v", got)
			}
		})
	}
}

// namespaceFile writes a service account namespace file naming
// testNamespace and returns its path.
func namespaceFile(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "namespace")
	if err := os.WriteFile(path, []byte(testNamespace), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

// newFakeClient returns a fake client holding objects, with the status
// subresource of both kinds, and with intercept's functions when it's set.
func newFakeClient(t *testing.T, intercept *interceptor.Funcs, objects ...client.Object) client.Client {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	builder := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		WithStatusSubresource(&v1alpha1.Shortlink{}, &v1alpha1.Redirect{})

	if intercept != nil {
		builder = builder.WithInterceptorFuncs(*intercept)
	}

	return builder.Build()
}

func newShortlink(name, owner string) *v1alpha1.Shortlink {
	return &v1alpha1.Shortlink{
		Name: name, Namespace: testNamespace,
		Spec:   v1alpha1.ShortlinkSpec{Owner: owner, Target: "https://example.com"},
		Status: v1alpha1.ShortlinkStatus{Count: 41},
	}
}

func newRedirect(name, namespace string) *v1alpha1.Redirect {
	return &v1alpha1.Redirect{
		Name: name, Namespace: namespace,
		Spec: v1alpha1.RedirectSpec{Source: name + ".example.com", Target: "https://example.com", Code: 308},
	}
}

// getShortlink reads the "home" ShortLink the tests act on.
func getShortlink(t *testing.T, k8s client.Client) *v1alpha1.Shortlink {
	t.Helper()

	shortlink := &v1alpha1.Shortlink{}
	if err := k8s.Get(context.Background(), types.NamespacedName{Name: "home", Namespace: testNamespace}, shortlink); err != nil {
		t.Fatal(err)
	}

	return shortlink
}

func lenRedirects(list *v1alpha1.RedirectList) int {
	if list == nil {
		return 0
	}

	return len(list.Items)
}

// checkErr fails t unless err is set exactly when wantErr is, is a not found
// error exactly when wantNotFound is, and carries advice.
func checkErr(t *testing.T, err error, wantErr, wantNotFound bool) {
	t.Helper()

	if (err != nil) != wantErr {
		t.Fatalf("error = %v, wantErr %v", err, wantErr)
	}

	if k8serrors.IsNotFound(err) != wantNotFound {
		t.Errorf("IsNotFound(%v) = %v, want %v", err, !wantNotFound, wantNotFound)
	}
}

func failList(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
	return errInjected
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
