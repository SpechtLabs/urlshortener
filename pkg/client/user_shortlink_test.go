package client

import (
	"context"
	"path/filepath"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/spechtlabs/urlshortener/api/v1alpha1"
)

func TestUserShortLinkClientList(t *testing.T) {
	tests := []struct {
		name      string
		user      string
		file      string
		wantNames []string
		wantErr   bool
	}{
		{name: "owned and co-owned", user: "octocat", file: namespaceFile(t), wantNames: []string{"blog", "home"}},
		{name: "owns nothing", user: "nobody", file: namespaceFile(t), wantErr: true},
		{name: "outside a pod", user: "octocat", file: filepath.Join(t.TempDir(), "missing"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blog := newShortlink("blog", "hubot")
			blog.Spec.CoOwners = []string{"octocat"}

			c := newUserClient(t, tt.file, nil, newShortlink("home", "octocat"), blog, newShortlink("docs", "hubot"))

			got, err := c.List(context.Background(), tt.user)
			if (err != nil) != tt.wantErr {
				t.Fatalf("List() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr {
				return
			}

			names := make([]string, 0, len(got.Items))
			for _, item := range got.Items {
				names = append(names, item.Name)
			}

			if len(names) != len(tt.wantNames) || names[0] != tt.wantNames[0] || names[1] != tt.wantNames[1] {
				t.Errorf("List() = %v, want %v", names, tt.wantNames)
			}
		})
	}
}

func TestUserShortLinkClientGet(t *testing.T) {
	tests := []struct {
		name         string
		user         string
		shortlink    string
		wantErr      bool
		wantNotFound bool
	}{
		{name: "owner", user: "octocat", shortlink: "home"},
		{name: "not the owner", user: "hubot", shortlink: "home", wantErr: true},
		{name: "missing", user: "octocat", shortlink: "missing", wantErr: true, wantNotFound: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newUserClient(t, namespaceFile(t), nil, newShortlink("home", "octocat"))

			got, err := c.Get(context.Background(), tt.user, tt.shortlink)
			checkErr(t, err, tt.wantErr, tt.wantNotFound)

			if !tt.wantErr && got.Name != tt.shortlink {
				t.Errorf("Get() = %q, want %q", got.Name, tt.shortlink)
			}
		})
	}
}

func TestUserShortLinkClientCreate(t *testing.T) {
	k8s := newFakeClient(t, nil)
	c := NewUserShortLinkClient(NewShortlinkClient(k8s, WithNamespaceFile(namespaceFile(t))))

	shortlink := &v1alpha1.Shortlink{
		Name: "home",
		Spec: v1alpha1.ShortlinkSpec{Owner: "someone else", Target: "https://example.com"},
	}

	if err := c.Create(context.Background(), "octocat", shortlink); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if got := getShortlink(t, k8s); got.Spec.Owner != "octocat" {
		t.Errorf("owner = %q, want the user who created it", got.Spec.Owner)
	}
}

func TestUserShortLinkClientUpdate(t *testing.T) {
	tests := []struct {
		name          string
		user          string
		intercept     *interceptor.Funcs
		wantErr       bool
		wantChangedBy string
	}{
		{name: "owner", user: "octocat", wantChangedBy: "octocat"},
		{name: "not the owner", user: "hubot", wantErr: true},
		{name: "update fails", user: "octocat", intercept: &interceptor.Funcs{Update: failUpdate}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k8s := newFakeClient(t, tt.intercept, newShortlink("home", "octocat"))
			c := NewUserShortLinkClient(NewShortlinkClient(k8s))

			shortlink := getShortlink(t, k8s)
			shortlink.Spec.Target = "https://example.org"

			err := c.Update(context.Background(), tt.user, shortlink)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Update() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr {
				return
			}

			got := getShortlink(t, k8s)
			if got.Spec.Target != "https://example.org" || got.Status.ChangedBy != tt.wantChangedBy {
				t.Errorf("after Update(): target %q, changedBy %q", got.Spec.Target, got.Status.ChangedBy)
			}
		})
	}
}

func TestUserShortLinkClientDelete(t *testing.T) {
	tests := []struct {
		name    string
		user    string
		wantErr bool
	}{
		{name: "owner", user: "octocat"},
		{name: "not the owner", user: "hubot", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k8s := newFakeClient(t, nil, newShortlink("home", "octocat"))
			c := NewUserShortLinkClient(NewShortlinkClient(k8s))

			if err := c.Delete(context.Background(), tt.user, getShortlink(t, k8s)); (err != nil) != tt.wantErr {
				t.Errorf("Delete() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func newUserClient(t *testing.T, file string, intercept *interceptor.Funcs, objects ...client.Object) *UserShortLinkClient {
	t.Helper()

	return NewUserShortLinkClient(NewShortlinkClient(newFakeClient(t, intercept, objects...), WithNamespaceFile(file)))
}
