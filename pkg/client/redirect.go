package client

import (
	"context"
	"fmt"

	"github.com/sierrasoftworks/humane-errors-go"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/spechtlabs/urlshortener/api/v1alpha1"
)

// RedirectClient is a Kubernetes client for easy CRUD operations
type RedirectClient struct {
	client client.Client
	tracer trace.Tracer
	settings
}

// NewRedirectClient creates a new Redirect Client
func NewRedirectClient(k8sClient client.Client, opts ...Option) *RedirectClient {
	return &RedirectClient{
		client:   k8sClient,
		tracer:   otel.Tracer("urlshortener"),
		settings: newSettings(opts),
	}
}

// Get returns a Redirect in the current namespace
func (c *RedirectClient) Get(ct context.Context, name string) (*v1alpha1.Redirect, humane.Error) {
	ctx, span := c.tracer.Start(ct, "RedirectClient.Get", trace.WithAttributes(attribute.String("name", name)))
	defer span.End()

	namespace, err := currentNamespace(c.namespaceFile)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	return c.GetNamespaced(ctx, types.NamespacedName{Name: name, Namespace: namespace})
}

// GetNameNamespace returns a Redirect for a given name in a given namespace
func (c *RedirectClient) GetNameNamespace(ct context.Context, name, namespace string) (*v1alpha1.Redirect, humane.Error) {
	ctx, span := c.tracer.Start(ct, "RedirectClient.GetNameNamespace", trace.WithAttributes(attribute.String("name", name), attribute.String("namespace", namespace)))
	defer span.End()

	return c.GetNamespaced(ctx, types.NamespacedName{Name: name, Namespace: namespace})
}

// GetNamespaced returns a Redirect
func (c *RedirectClient) GetNamespaced(ct context.Context, nameNamespaced types.NamespacedName) (*v1alpha1.Redirect, humane.Error) {
	ctx, span := c.tracer.Start(
		ct,
		"RedirectClient.GetNamespaced",
		trace.WithAttributes(
			attribute.String("name", nameNamespaced.Name),
			attribute.String("namespace", nameNamespaced.Namespace),
		),
	)
	defer span.End()

	redirect := &v1alpha1.Redirect{}

	if err := c.client.Get(ctx, nameNamespaced, redirect); err != nil {
		span.RecordError(err)
		return nil, humane.Wrap(err, fmt.Sprintf("Unable to get Redirect %s", nameNamespaced),
			"Check that the Redirect exists and the controller's service account may read redirects.urlshortener.cedi.dev",
		)
	}

	return redirect, nil
}

// ListAll returns a list of all Redirect
func (c *RedirectClient) ListAll(ct context.Context) (*v1alpha1.RedirectList, humane.Error) {
	ctx, span := c.tracer.Start(ct, "RedirectClient.List")
	defer span.End()

	redirects := &v1alpha1.RedirectList{}

	if err := c.client.List(ctx, redirects); err != nil {
		span.RecordError(err)
		return nil, humane.Wrap(err, "Unable to list Redirects",
			"Check that the controller's service account may list redirects.urlshortener.cedi.dev in every namespace",
		)
	}

	return redirects, nil
}

// List returns a list of all Redirect in the current namespace
func (c *RedirectClient) List(ct context.Context) (*v1alpha1.RedirectList, humane.Error) {
	ctx, span := c.tracer.Start(ct, "RedirectClient.List")
	defer span.End()

	namespace, err := currentNamespace(c.namespaceFile)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	return c.ListNamespaced(ctx, namespace)
}

// ListNamespaced returns a list of Redirects in a Namespace
func (c *RedirectClient) ListNamespaced(ct context.Context, namespace string) (*v1alpha1.RedirectList, humane.Error) {
	ctx, span := c.tracer.Start(
		ct,
		"RedirectClient.List",
		trace.WithAttributes(
			attribute.String("namespace", namespace),
		),
	)
	defer span.End()

	redirects := &v1alpha1.RedirectList{}

	if err := c.client.List(ctx, redirects, &client.ListOptions{Namespace: namespace}); err != nil {
		span.RecordError(err)
		return nil, humane.Wrap(err, fmt.Sprintf("Unable to list Redirects in namespace %s", namespace),
			"Check that the controller's service account may list redirects.urlshortener.cedi.dev",
		)
	}

	return redirects, nil
}

// Query returns a list of all Redirect that match the label Redirect with the parameter label
// TODO(cedi): Rewrite and come up with a better way. This only works client-side and is absolutely ugly and inefficient
func (c *RedirectClient) Query(ct context.Context, label string) (*v1alpha1.RedirectList, humane.Error) {
	ctx, span := c.tracer.Start(ct, "RedirectClient.Query", trace.WithAttributes(attribute.String("label", "Redirect"), attribute.String("labelValue", label)))
	defer span.End()

	redirects := &v1alpha1.RedirectList{}

	// Like `kubectl get Redirect -l Redirect=$Redirect
	redirectReq, err := labels.NewRequirement("Redirect", selection.Equals, []string{label})
	if err != nil {
		span.RecordError(err)
		return nil, humane.Wrap(err, fmt.Sprintf("Invalid Redirect label %q", label),
			"Label values must be 63 characters or less and consist of alphanumerics, '-', '_' or '.'",
		)
	}

	selector := labels.NewSelector().Add(*redirectReq)

	if err := c.client.List(ctx, redirects, &client.ListOptions{LabelSelector: selector}); err != nil {
		span.RecordError(err)
		return nil, humane.Wrap(err, fmt.Sprintf("Unable to list Redirects labeled %q", label),
			"Check that the controller's service account may list redirects.urlshortener.cedi.dev",
		)
	}

	return redirects, nil
}

// Save writes the Redirect's spec and metadata
func (c *RedirectClient) Save(ct context.Context, redirect *v1alpha1.Redirect) humane.Error {
	ctx, span := c.tracer.Start(ct, "RedirectClient.Save", trace.WithAttributes(attribute.String("Redirect", redirect.Name), attribute.String("namespace", redirect.Namespace)))
	defer span.End()

	if err := c.client.Update(ctx, redirect); err != nil {
		span.RecordError(err)
		return humane.Wrap(err, fmt.Sprintf("Unable to update Redirect %s", redirect.Name),
			"Check that the Redirect still exists and wasn't changed concurrently, then try again",
		)
	}

	return nil
}

// SaveStatus writes the Redirect's status
func (c *RedirectClient) SaveStatus(ct context.Context, redirect *v1alpha1.Redirect) humane.Error {
	ctx, span := c.tracer.Start(ct, "RedirectClient.SaveStatus", trace.WithAttributes(attribute.String("Redirect", redirect.Name), attribute.String("namespace", redirect.Namespace)))
	defer span.End()

	if err := c.client.Status().Update(ctx, redirect); err != nil {
		span.RecordError(err)
		return humane.Wrap(err, fmt.Sprintf("Unable to update the status of Redirect %s", redirect.Name),
			"Check that the Redirect still exists and wasn't changed concurrently, then try again",
		)
	}

	return nil
}
