package client

import (
	"context"
	"fmt"

	"github.com/sierrasoftworks/humane-errors-go"

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
	settings
}

// NewShortlinkClient creates a new shortlink Client
func NewShortlinkClient(k8sClient client.Client, opts ...Option) *ShortlinkClient {
	return &ShortlinkClient{
		client:   k8sClient,
		tracer:   otel.Tracer("urlshortener"),
		settings: newSettings(opts),
	}
}

// Get returns a ShortLink in the current namespace
func (c *ShortlinkClient) Get(ct context.Context, name string) (*v1alpha1.Shortlink, humane.Error) {
	ctx, span := c.tracer.Start(ct, "ShortlinkClient.Get", trace.WithAttributes(attribute.String("name", name)))
	defer span.End()

	namespace, err := currentNamespace(c.namespaceFile)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	return c.GetNamespaced(ctx, types.NamespacedName{Name: name, Namespace: namespace})
}

// GetNameNamespace returns a Shortlink for a given name in a given namespace
func (c *ShortlinkClient) GetNameNamespace(ct context.Context, name, namespace string) (*v1alpha1.Shortlink, humane.Error) {
	ctx, span := c.tracer.Start(ct, "ShortlinkClient.GetNameNamespace", trace.WithAttributes(attribute.String("name", name), attribute.String("namespace", namespace)))
	defer span.End()

	return c.GetNamespaced(ctx, types.NamespacedName{Name: name, Namespace: namespace})
}

// GetNamespaced returns a Shortlink
func (c *ShortlinkClient) GetNamespaced(ct context.Context, nameNamespaced types.NamespacedName) (*v1alpha1.Shortlink, humane.Error) {
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
		return nil, humane.Wrap(err, fmt.Sprintf("Unable to get ShortLink %s", nameNamespaced),
			"Check that the ShortLink exists and the server's service account may read shortlinks.urlshortener.cedi.dev",
		)
	}

	return shortlink, nil
}

// List returns a list of all Shortlinks in the current namespace
func (c *ShortlinkClient) List(ct context.Context) (*v1alpha1.ShortlinkList, humane.Error) {
	ctx, span := c.tracer.Start(ct, "ShortlinkClient.List")
	defer span.End()

	namespace, err := currentNamespace(c.namespaceFile)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	return c.ListNamespaced(ctx, namespace)
}

// ListNamespaced returns a list of all Shortlinks in a namespace
func (c *ShortlinkClient) ListNamespaced(ct context.Context, namespace string) (*v1alpha1.ShortlinkList, humane.Error) {
	ctx, span := c.tracer.Start(ct, "ShortlinkClient.ListNamespaced", trace.WithAttributes(attribute.String("namespace", namespace)))
	defer span.End()

	shortlinks := &v1alpha1.ShortlinkList{}

	if err := c.client.List(ctx, shortlinks, &client.ListOptions{Namespace: namespace}); err != nil {
		span.RecordError(err)
		return nil, humane.Wrap(err, fmt.Sprintf("Unable to list ShortLinks in namespace %s", namespace),
			"Check that the server's service account may list shortlinks.urlshortener.cedi.dev",
		)
	}

	return shortlinks, nil
}

// Update writes the ShortLink's spec and metadata
func (c *ShortlinkClient) Update(ct context.Context, shortlink *v1alpha1.Shortlink) humane.Error {
	ctx, span := c.tracer.Start(ct, "ShortlinkClient.Update", trace.WithAttributes(attribute.String("shortlink", shortlink.Name), attribute.String("namespace", shortlink.Namespace)))
	defer span.End()

	if err := c.client.Update(ctx, shortlink); err != nil {
		span.RecordError(err)
		return humane.Wrap(err, fmt.Sprintf("Unable to update ShortLink %s", shortlink.Name),
			"Check that the ShortLink still exists and wasn't changed concurrently, then try again",
		)
	}

	return nil
}

// UpdateStatus writes the ShortLink's status
func (c *ShortlinkClient) UpdateStatus(ct context.Context, shortlink *v1alpha1.Shortlink) humane.Error {
	ctx, span := c.tracer.Start(ct, "ShortlinkClient.UpdateStatus", trace.WithAttributes(attribute.String("shortlink", shortlink.Name), attribute.String("namespace", shortlink.Namespace)))
	defer span.End()

	if err := c.client.Status().Update(ctx, shortlink); err != nil {
		span.RecordError(err)
		return humane.Wrap(err, fmt.Sprintf("Unable to update the status of ShortLink %s", shortlink.Name),
			"Check that the ShortLink still exists and wasn't changed concurrently, then try again",
		)
	}

	return nil
}

// IncrementInvocationCount counts one more invocation in the ShortLink's status
func (c *ShortlinkClient) IncrementInvocationCount(ct context.Context, shortlink *v1alpha1.Shortlink) humane.Error {
	ctx, span := c.tracer.Start(ct, "ShortlinkClient.IncrementInvocationCount", trace.WithAttributes(attribute.String("shortlink", shortlink.Name), attribute.String("namespace", shortlink.Namespace)))
	defer span.End()

	shortlink.Status.Count++

	if err := c.client.Status().Update(ctx, shortlink); err != nil {
		span.RecordError(err)
		return humane.Wrap(err, fmt.Sprintf("Unable to count the invocation of ShortLink %s", shortlink.Name),
			"The redirect was served; only its count in the ShortLink's status is missing",
		)
	}

	return nil
}

// Delete deletes the ShortLink
func (c *ShortlinkClient) Delete(ct context.Context, shortlink *v1alpha1.Shortlink) humane.Error {
	ctx, span := c.tracer.Start(ct, "ShortlinkClient.Delete", trace.WithAttributes(attribute.String("name", shortlink.Name), attribute.String("namespace", shortlink.Namespace)))
	defer span.End()

	if err := c.client.Delete(ctx, shortlink); err != nil {
		span.RecordError(err)
		return humane.Wrap(err, fmt.Sprintf("Unable to delete ShortLink %s", shortlink.Name),
			"Check that the ShortLink still exists and the server's service account may delete shortlinks.urlshortener.cedi.dev",
		)
	}

	return nil
}

// Create creates the ShortLink, in the current namespace unless it names one
func (c *ShortlinkClient) Create(ct context.Context, shortlink *v1alpha1.Shortlink) humane.Error {
	ctx, span := c.tracer.Start(ct, "ShortlinkClient.Create", trace.WithAttributes(attribute.String("shortlink", shortlink.Name), attribute.String("namespace", shortlink.Namespace)))
	defer span.End()

	if shortlink.Namespace == "" {
		namespace, err := currentNamespace(c.namespaceFile)
		if err != nil {
			span.RecordError(err)
			return err
		}

		shortlink.Namespace = namespace
	}

	if err := c.client.Create(ctx, shortlink); err != nil {
		span.RecordError(err)
		return humane.Wrap(err, fmt.Sprintf("Unable to create ShortLink %s", shortlink.Name),
			"Check that no ShortLink of that name exists yet and the spec is valid",
		)
	}

	return nil
}
