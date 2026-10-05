/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/sierrasoftworks/humane-errors-go"
	"github.com/spechtlabs/go-otel-utils/otelzap"

	v1alpha1 "github.com/spechtlabs/urlshortener/api/v1alpha1"
	rClient "github.com/spechtlabs/urlshortener/pkg/client"
)

// RedirectReconciler reconciles a Redirect object
type RedirectReconciler struct {
	client  client.Client
	rClient *rClient.RedirectClient

	scheme *runtime.Scheme
	tracer trace.Tracer
}

// NewRedirectReconciler returns a new RedirectReconciler
func NewRedirectReconciler(k8sClient client.Client, scheme *runtime.Scheme) *RedirectReconciler {
	return &RedirectReconciler{
		client:  k8sClient,
		rClient: rClient.NewRedirectClient(k8sClient),
		scheme:  scheme,
		tracer:  otel.Tracer("urlshortener"),
	}
}

// +kubebuilder:rbac:groups=urlshortener.cedi.dev,resources=redirects,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=urlshortener.cedi.dev,resources=redirects/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=urlshortener.cedi.dev,resources=redirects/finalizers,verbs=update
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// the Redirect object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.4/pkg/reconcile
func (r *RedirectReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	defer timeReconcile("redirect", req).ObserveDuration()

	span := trace.SpanFromContext(ctx)

	// Check if the span was sampled and is recording the data
	if !span.IsRecording() {
		ctx, span = r.tracer.Start(ctx, "RedirectReconciler.Reconcile")
		defer span.End()
	}

	span.SetAttributes(attribute.String("redirect", req.String()))

	// Monitor the number of redirects
	if redirectList, err := r.rClient.List(ctx); redirectList != nil && err == nil {
		active.WithLabelValues("redirect").Set(float64(len(redirectList.Items)))
	}

	// get Redirect from etcd
	redirect, err := r.rClient.GetNamespaced(ctx, req.NamespacedName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			otelzap.L().WithError(err).DebugContext(ctx, "Redirect resource not found. Ignoring since object must be deleted",
				zap.String("name", "reconciler"),
				zap.String("redirect", req.String()),
			)
			return ctrl.Result{}, nil
		}

		// Error reading the object - requeue the request.
		return ctrl.Result{}, r.fail(ctx, req, err)
	}

	// Create the ingress, or bring the existing one in line with the Redirect
	ingress, err := r.upsertRedirectIngress(ctx, redirect)
	if err != nil {
		return ctrl.Result{}, r.fail(ctx, req, err)
	}

	// Update the Redirect status with the ingress name and the target
	ingressList := &networkingv1.IngressList{}
	listOpts := []client.ListOption{
		client.InNamespace(redirect.Namespace),
		client.MatchingLabels(GetLabelsForRedirect(redirect.Name)),
	}

	if err := r.client.List(ctx, ingressList, listOpts...); err != nil {
		return ctrl.Result{}, r.fail(ctx, req, humane.Wrap(err, "Failed to list the Redirect's ingresses",
			"Check that the controller's service account may list ingresses.networking.k8s.io",
		))
	}

	redirect.Status.IngressName = GetIngressNames(ingressList.Items)
	redirect.Status.Target = ingress.Annotations[permanentRedirectAnnotation]
	if err := r.rClient.SaveStatus(ctx, redirect); err != nil {
		return ctrl.Result{}, r.fail(ctx, req, err)
	}

	return ctrl.Result{}, nil
}

// fail logs a reconcile error and returns it, so controller-runtime requeues
// the request.
func (r *RedirectReconciler) fail(ctx context.Context, req ctrl.Request, err humane.Error) humane.Error {
	span := trace.SpanFromContext(ctx)
	span.RecordError(err)
	span.SetAttributes(attribute.StringSlice("error.advice", err.Advice()))

	otelzap.L().WithError(err).ErrorContext(ctx, err.Error(),
		zap.String("name", "reconciler"),
		zap.String("redirect", req.String()),
	)

	return err
}

// upsertRedirectIngress creates the Redirect's Ingress, or updates the one
// that exists, and returns it as it is in the cluster.
func (r *RedirectReconciler) upsertRedirectIngress(ctx context.Context, redirect *v1alpha1.Redirect) (*networkingv1.Ingress, humane.Error) {
	ingress := &networkingv1.Ingress{
		Name: redirect.Name, Namespace: redirect.Namespace,
	}

	var mutateErr humane.Error
	if _, err := controllerutil.CreateOrUpdate(ctx, r.client, ingress, func() error {
		mutateErr = UpdateRedirectIngress(ingress, redirect, r.scheme)
		return mutateErr
	}); err != nil {
		if mutateErr != nil {
			return nil, mutateErr
		}

		return nil, humane.Wrap(err, "Failed to create or update the redirect Ingress",
			"Check that the controller's service account may create and update ingresses.networking.k8s.io",
		)
	}

	return ingress, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RedirectReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Redirect{}).
		Named("redirect").
		Complete(r)
}
