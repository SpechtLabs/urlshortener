package client

import (
	"context"

	"github.com/sierrasoftworks/humane-errors-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/spechtlabs/urlshortener/api/v1alpha1"
)

// UserShortLinkClient acts on ShortLinks on behalf of a GitHub user, who may
// only see and change the ShortLinks they own or co-own.
type UserShortLinkClient struct {
	tracer trace.Tracer
	client *ShortlinkClient
}

// NewUserShortLinkClient returns a UserShortLinkClient that reads and writes
// through shortlinkClient.
func NewUserShortLinkClient(shortlinkClient *ShortlinkClient) *UserShortLinkClient {
	return &UserShortLinkClient{
		tracer: otel.Tracer("urlshortener"),
		client: shortlinkClient,
	}
}

// List returns the ShortLinks in the current namespace that username owns or
// co-owns.
func (c *UserShortLinkClient) List(ct context.Context, username string) (*v1alpha1.ShortlinkList, humane.Error) {
	ctx, span := c.tracer.Start(ct, "UserShortLinkClient.List")
	defer span.End()

	list, err := c.client.List(ctx)
	if err != nil {
		return nil, err
	}

	userShortlinkList := v1alpha1.ShortlinkList{
		TypeMeta: list.TypeMeta,
		ListMeta: list.ListMeta,
		Items:    make([]v1alpha1.Shortlink, 0),
	}

	for _, shortLink := range list.Items {
		if shortLink.IsOwnedBy(username) {
			userShortlinkList.Items = append(userShortlinkList.Items, shortLink)
		}
	}

	if len(userShortlinkList.Items) == 0 {
		return nil, NewNotAllowedError(username, ReadOperation, "all shortlinks")
	}

	return &userShortlinkList, nil
}

// Get returns the ShortLink name in the current namespace, if username owns or
// co-owns it.
func (c *UserShortLinkClient) Get(ct context.Context, username string, name string) (*v1alpha1.Shortlink, humane.Error) {
	ctx, span := c.tracer.Start(ct, "UserShortLinkClient.Get")
	defer span.End()

	shortLink, err := c.client.Get(ctx, name)
	if err != nil {
		return nil, err
	}

	if !shortLink.IsOwnedBy(username) {
		return nil, NewNotAllowedError(username, ReadOperation, shortLink.Name)
	}

	return shortLink, nil
}

// Create creates shortLink with username as its owner.
func (c *UserShortLinkClient) Create(ct context.Context, username string, shortLink *v1alpha1.Shortlink) humane.Error {
	ctx, span := c.tracer.Start(ct, "UserShortLinkClient.Create")
	defer span.End()

	shortLink.Spec.Owner = username
	return c.client.Create(ctx, shortLink)
}

// Update writes shortLink, if username owns or co-owns it, and records
// username as the one who changed it last.
func (c *UserShortLinkClient) Update(ct context.Context, username string, shortLink *v1alpha1.Shortlink) humane.Error {
	ctx, span := c.tracer.Start(ct, "UserShortLinkClient.Update")
	defer span.End()

	if !shortLink.IsOwnedBy(username) {
		return NewNotAllowedError(username, UpdateOperation, shortLink.Name)
	}

	if err := c.client.Update(ctx, shortLink); err != nil {
		return err
	}

	shortLink.Status.ChangedBy = username
	return c.client.UpdateStatus(ctx, shortLink)
}

// Delete deletes shortLink, if username owns or co-owns it.
func (c *UserShortLinkClient) Delete(ct context.Context, username string, shortLink *v1alpha1.Shortlink) humane.Error {
	ctx, span := c.tracer.Start(ct, "UserShortLinkClient.Delete")
	defer span.End()

	if !shortLink.IsOwnedBy(username) {
		return NewNotAllowedError(username, DeleteOperation, shortLink.Name)
	}

	return c.client.Delete(ctx, shortLink)
}
