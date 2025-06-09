package client

import (
	"context"
	"os"

	"github.com/pkg/errors"

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
}

// NewRedirectClient creates a new Redirect Client
func NewRedirectClient(client client.Client) *RedirectClient {
	return &RedirectClient{
		client: client,
		tracer: otel.Tracer("urlshortener"),
	}
}

func (c *RedirectClient) Get(ct context.Context, name string) (*v1alpha1.Redirect, error) {
	ctx, span := c.tracer.Start(ct, "RedirectClient.Get", trace.WithAttributes(attribute.String("name", name)))
	defer span.End()

	// try to read the namespace from /var/run
	namespace, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		span.RecordError(err)
		return nil, errors.Wrap(err, "Unable to read current namespace")
	}

	return c.GetNamespaced(ctx, types.NamespacedName{Name: name, Namespace: string(namespace)})
}

// GetNameNamespace returns a Redirect for a given name in a given namespace
func (c *RedirectClient) GetNameNamespace(ct context.Context, name, namespace string) (*v1alpha1.Redirect, error) {
	ctx, span := c.tracer.Start(ct, "RedirectClient.GetNameNamespace", trace.WithAttributes(attribute.String("name", name), attribute.String("namespace", namespace)))
	defer span.End()

	return c.GetNamespaced(ctx, types.NamespacedName{Name: name, Namespace: namespace})
}

// GetNamespaced returns a Redirect
func (c *RedirectClient) GetNamespaced(ct context.Context, nameNamespaced types.NamespacedName) (*v1alpha1.Redirect, error) {
	ctx, span := c.tracer.Start(
		ct,
		"RedirectClient.GetNamespaced",
		trace.WithAttributes(
			attribute.String("name", nameNamespaced.Name),
			attribute.String("namespace", nameNamespaced.Namespace),
		),
	)
	defer span.End()

	Redirect := &v1alpha1.Redirect{}

	err := c.client.Get(ctx, nameNamespaced, Redirect)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	return Redirect, nil
}

// ListAll returns a list of all Redirect
func (c *RedirectClient) ListAll(ct context.Context) (*v1alpha1.RedirectList, error) {
	ctx, span := c.tracer.Start(ct, "RedirectClient.List")
	defer span.End()

	Redirects := &v1alpha1.RedirectList{}

	err := c.client.List(ctx, Redirects)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	return Redirects, nil
}

// List returns a list of all Redirect in the current namespace
func (c *RedirectClient) List(ct context.Context) (*v1alpha1.RedirectList, error) {
	ctx, span := c.tracer.Start(ct, "RedirectClient.List")
	defer span.End()

	// try to read the namespace from /var/run
	namespace, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		span.RecordError(err)
		return nil, errors.Wrap(err, "Unable to read current namespace")
	}

	return c.ListNamespaced(ctx, string(namespace))
}

// List returns a list of Redirects in a Namespace
func (c *RedirectClient) ListNamespaced(ct context.Context, namespace string) (*v1alpha1.RedirectList, error) {
	ctx, span := c.tracer.Start(
		ct,
		"RedirectClient.List",
		trace.WithAttributes(
			attribute.String("namespace", namespace),
		),
	)
	defer span.End()

	Redirects := &v1alpha1.RedirectList{}

	err := c.client.List(ctx, Redirects, &client.ListOptions{
		Namespace: namespace,
	})
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	return Redirects, nil
}

// List returns a list of all Redirect that match the label Redirect with the parameter label
// ToDo: Rewrite and come up with a better way. This only works client-side and is absolutely ugly and inefficient
func (c *RedirectClient) Query(ct context.Context, label string) (*v1alpha1.RedirectList, error) {
	ctx, span := c.tracer.Start(ct, "RedirectClient.Query", trace.WithAttributes(attribute.String("label", "Redirect"), attribute.String("labelValue", label)))
	defer span.End()

	Redirects := &v1alpha1.RedirectList{}

	// Like `kubectl get Redirect -l Redirect=$Redirect
	RedirectReq, _ := labels.NewRequirement("Redirect", selection.Equals, []string{label})
	selector := labels.NewSelector()
	selector = selector.Add(*RedirectReq)

	err := c.client.List(ctx, Redirects, &client.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	return Redirects, nil
}

func (c *RedirectClient) Save(ct context.Context, Redirect *v1alpha1.Redirect) error {
	ctx, span := c.tracer.Start(ct, "RedirectClient.Save", trace.WithAttributes(attribute.String("Redirect", Redirect.Name), attribute.String("namespace", Redirect.Namespace)))
	defer span.End()

	err := c.client.Update(ctx, Redirect)
	if err != nil {
		span.RecordError(err)
	}

	return err
}

func (c *RedirectClient) SaveStatus(ct context.Context, Redirect *v1alpha1.Redirect) error {
	ctx, span := c.tracer.Start(ct, "RedirectClient.SaveStatus", trace.WithAttributes(attribute.String("Redirect", Redirect.Name), attribute.String("namespace", Redirect.Namespace)))
	defer span.End()

	err := c.client.Status().Update(ctx, Redirect)
	if err != nil {
		span.RecordError(err)
	}

	return err
}
