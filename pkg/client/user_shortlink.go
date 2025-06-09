package client

import (
	"context"

	"github.com/spechtlabs/urlshortener/api/v1alpha1"
	"go.opentelemetry.io/otel"

	"github.com/pkg/errors"
	"go.opentelemetry.io/otel/trace"
)

type UserShortLinkClient struct {
	tracer trace.Tracer
	client *ShortlinkClient
}

func NewUserShortLinkClient(client *ShortlinkClient) *UserShortLinkClient {
	return &UserShortLinkClient{
		tracer: otel.Tracer("urlshortener"),
		client: client,
	}
}

func (c *UserShortLinkClient) List(ct context.Context, username string) (*v1alpha1.ShortlinkList, error) {
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

func (c *UserShortLinkClient) Get(ct context.Context, username string, name string) (*v1alpha1.Shortlink, error) {
	ctx, span := c.tracer.Start(ct, "UserShortLinkClient.Get")
	defer span.End()

	shortLink, err := c.client.Get(ctx, name)
	if err != nil {
		return nil, errors.Wrap(err, "Unable to get shortlink")
	}

	if !shortLink.IsOwnedBy(username) {
		return nil, NewNotAllowedError(username, ReadOperation, shortLink.Name)
	}

	return shortLink, nil
}

func (c *UserShortLinkClient) Create(ct context.Context, username string, shortLink *v1alpha1.Shortlink) error {
	ctx, span := c.tracer.Start(ct, "UserShortLinkClient.Create")
	defer span.End()

	shortLink.Spec.Owner = username
	return c.client.Create(ctx, shortLink)
}

func (c *UserShortLinkClient) Update(ct context.Context, username string, shortLink *v1alpha1.Shortlink) error {
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

func (c *UserShortLinkClient) Delete(ct context.Context, username string, shortLink *v1alpha1.Shortlink) error {
	ctx, span := c.tracer.Start(ct, "UserShortLinkClient.Update")
	defer span.End()

	if !shortLink.IsOwnedBy(username) {
		return NewNotAllowedError(username, DeleteOperation, shortLink.Name)
	}

	return c.client.Delete(ctx, shortLink)
}
