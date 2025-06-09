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
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/pkg/errors"
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
func NewRedirectReconciler(client client.Client, scheme *runtime.Scheme) *RedirectReconciler {
	return &RedirectReconciler{
		client:  client,
		rClient: rClient.NewRedirectClient(client),
		scheme:  scheme,
		tracer:  otel.Tracer("urlshortener"),
	}
}

// +kubebuilder:rbac:groups=urlshortener.cedi.dev,resources=redirects,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=urlshortener.cedi.dev,resources=redirects/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=urlshortener.cedi.dev,resources=redirects/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// the Redirect object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.4/pkg/reconcile
func (r *RedirectReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	startTime := time.Now()
	defer func() {
		reconcilerDuration.WithLabelValues("redirect", req.Name, req.Namespace).Observe(float64(time.Since(startTime).Microseconds()))
	}()

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
			otelzap.L().WithError(err).Ctx(ctx).Info("Redirect resource not found. Ignoring since object must be deleted",
				zap.String("name", "reconciler"),
				zap.String("redirect", req.String()),
			)
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return ctrl.Result{}, nil
		}

		// Error reading the object - requeue the request.
		otelzap.L().WithError(err).Ctx(ctx).Error("Failed to fetch Redirect resource",
			zap.String("name", "reconciler"),
			zap.String("redirect", req.String()),
		)
		return ctrl.Result{}, err
	}

	// Check if the ingress already exists, if not create a new one
	ingress, err := r.upsertRedirectIngress(ctx, redirect)
	if err != nil {
		otelzap.L().WithError(err).Ctx(ctx).Error("Failed to upsert redirect ingress",
			zap.String("name", "reconciler"),
			zap.String("redirect", req.String()),
		)
	}

	// Update the Redirect status with the ingress name and the target
	ingressList := &networkingv1.IngressList{}
	listOpts := []client.ListOption{
		client.InNamespace(redirect.Namespace),
		client.MatchingLabels(GetLabelsForRedirect(redirect.Name)),
	}

	if err = r.client.List(ctx, ingressList, listOpts...); err != nil {
		otelzap.L().WithError(err).Ctx(ctx).Error("Failed to list ingresses",
			zap.String("name", "reconciler"),
			zap.String("redirect", req.String()),
		)
		return ctrl.Result{}, err
	}

	// Update status.Nodes if needed
	redirect.Status.IngressName = GetIngressNames(ingressList.Items)
	redirect.Status.Target = ingress.Annotations["nginx.ingress.kubernetes.io/permanent-redirect"]
	err = r.client.Status().Update(ctx, redirect)
	if err != nil {
		otelzap.L().WithError(err).Ctx(ctx).Error("Failed to update Redirect status",
			zap.String("name", "reconciler"),
			zap.String("redirect", req.String()),
		)
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *RedirectReconciler) upsertRedirectIngress(ctx context.Context, redirect *v1alpha1.Redirect) (*networkingv1.Ingress, error) {
	ingress := &networkingv1.Ingress{}
	err := r.client.Get(ctx, types.NamespacedName{Name: redirect.Name, Namespace: redirect.Namespace}, ingress)
	ingress = UpdateRedirectIngress(ingress, redirect, r.scheme)

	if err != nil && k8serrors.IsNotFound(err) {
		if err := r.client.Create(ctx, ingress); err != nil {
			return nil, errors.Wrap(err, "Failed to create new Ingress")
		}
	} else if err != nil {
		return nil, errors.Wrap(err, "Failed to get redirect Ingress")
	}

	if err := r.client.Update(ctx, ingress); err != nil {
		return nil, errors.Wrap(err, "Failed to update redirect Ingress")
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
