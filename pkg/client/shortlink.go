package client

import (
	"context"
	"os"

	"github.com/pkg/errors"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/spechtlabs/urlshortener/api/v1alpha1"
)

// ShortlinkClient is a Kubernetes client for easy CRUD operations
type ShortlinkClient struct {
	client client.Client
	tracer trace.Tracer
}

// NewShortlinkClient creates a new shortlink Client
func NewShortlinkClient(client client.Client) *ShortlinkClient {
	return &ShortlinkClient{
		client: client,
		tracer: otel.Tracer("urlshortener"),
	}
}

// Get returns a ShortLink in the current namespace
func (c *ShortlinkClient) Get(ct context.Context, name string) (*v1alpha1.Shortlink, error) {
	ctx, span := c.tracer.Start(ct, "ShortlinkClient.Get", trace.WithAttributes(attribute.String("name", name)))
	defer span.End()

	// try to read the namespace from /var/run
	namespace, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		span.RecordError(err)
		return nil, errors.Wrap(err, "Unable to read current namespace")
	}

	return c.GetNamespaced(ctx, types.NamespacedName{Name: name, Namespace: string(namespace)})
}

// GetNameNamespace returns a Shortlink for a given name in a given namespace
func (c *ShortlinkClient) GetNameNamespace(ct context.Context, name, namespace string) (*v1alpha1.Shortlink, error) {
	ctx, span := c.tracer.Start(ct, "ShortlinkClient.GetNameNamespace", trace.WithAttributes(attribute.String("name", name), attribute.String("namespace", namespace)))
	defer span.End()

	return c.GetNamespaced(ctx, types.NamespacedName{Name: name, Namespace: namespace})
}

// Get returns a Shortlink
func (c *ShortlinkClient) GetNamespaced(ct context.Context, nameNamespaced types.NamespacedName) (*v1alpha1.Shortlink, error) {
	ctx, span := c.tracer.Start(
		ct, "ShortlinkClient.GetNamespaced",
		trace.WithAttributes(
			attribute.String("name", nameNamespaced.Name),
			attribute.String("namespace", nameNamespaced.Namespace),
		),
	)
	defer span.End()

	shortlink := &v1alpha1.Shortlink{}

	if err := c.client.Get(ctx, nameNamespaced, shortlink); err != nil {
		span.RecordError(err)
		return nil, err
	}

	return shortlink, nil
}

// List returns a list of all Shortlinks in the current namespace
func (c *ShortlinkClient) List(ct context.Context) (*v1alpha1.ShortlinkList, error) {
	ctx, span := c.tracer.Start(ct, "ShortlinkClient.List")
	defer span.End()

	// try to read the namespace from /var/run
	namespace, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		span.RecordError(err)
		return nil, errors.Wrap(err, "Unable to read current namespace")
	}

	return c.ListNamespaced(ctx, string(namespace))
}

// ListNamespaced returns a list of all Shortlinks in a namespace
func (c *ShortlinkClient) ListNamespaced(ct context.Context, namespace string) (*v1alpha1.ShortlinkList, error) {
	ctx, span := c.tracer.Start(ct, "ShortlinkClient.ListNamespaced", trace.WithAttributes(attribute.String("namespace", namespace)))
	defer span.End()

	shortlinks := &v1alpha1.ShortlinkList{}

	if err := c.client.List(ctx, shortlinks, &client.ListOptions{Namespace: namespace}); err != nil {
		span.RecordError(err)
		return nil, err
	}

	return shortlinks, nil
}

func (c *ShortlinkClient) Update(ct context.Context, shortlink *v1alpha1.Shortlink) error {
	ctx, span := c.tracer.Start(ct, "ShortlinkClient.Update", trace.WithAttributes(attribute.String("shortlink", shortlink.Name), attribute.String("namespace", shortlink.Namespace)))
	defer span.End()

	if err := c.client.Update(ctx, shortlink); err != nil {
		span.RecordError(err)
		return err
	}

	return nil
}

func (c *ShortlinkClient) UpdateStatus(ct context.Context, shortlink *v1alpha1.Shortlink) error {
	ctx, span := c.tracer.Start(ct, "ShortlinkClient.UpdateStatus", trace.WithAttributes(attribute.String("shortlink", shortlink.Name), attribute.String("namespace", shortlink.Namespace)))
	defer span.End()

	err := c.client.Status().Update(ctx, shortlink)
	if err != nil {
		span.RecordError(err)
	}

	return err
}

func (c *ShortlinkClient) IncrementInvocationCount(ct context.Context, shortlink *v1alpha1.Shortlink) error {
	ctx, span := c.tracer.Start(ct, "ShortlinkClient.IncrementInvocationCount", trace.WithAttributes(attribute.String("shortlink", shortlink.Name), attribute.String("namespace", shortlink.Namespace)))
	defer span.End()

	shortlink.Status.Count = shortlink.Status.Count + 1

	if err := c.client.Status().Update(ctx, shortlink); err != nil {
		span.RecordError(err)
		return err
	}

	return nil
}

func (c *ShortlinkClient) Delete(ct context.Context, shortlink *v1alpha1.Shortlink) error {
	ctx, span := c.tracer.Start(ct, "ShortlinkClient.Delete", trace.WithAttributes(attribute.String("name", shortlink.Name), attribute.String("namespace", shortlink.Namespace)))
	defer span.End()

	if err := c.client.Delete(ctx, shortlink); err != nil {
		span.RecordError(err)
		return err
	}

	return nil
}

func (c *ShortlinkClient) Create(ct context.Context, shortlink *v1alpha1.Shortlink) error {
	ctx, span := c.tracer.Start(ct, "ShortlinkClient.Create", trace.WithAttributes(attribute.String("shortlink", shortlink.Name), attribute.String("namespace", shortlink.Namespace)))
	defer span.End()

	if shortlink.Namespace == "" {
		// try to read the namespace from /var/run
		namespace, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
		if err != nil {
			span.RecordError(err)
			return errors.Wrap(err, "Unable to read current namespace")
		}

		shortlink.Namespace = string(namespace)
	}

	// if not exists, create a new one
	if err := c.client.Create(ctx, shortlink); err != nil {
		span.RecordError(err)
		return err
	}

	return nil
}
