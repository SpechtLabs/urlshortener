/*
Copyright 2022.

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

package controllers

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/instrument"
	"go.opentelemetry.io/otel/trace"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	urlshortenerv1alpha1 "github.com/cedi/urlshortener/api/v1alpha1"
	redirectclient "github.com/cedi/urlshortener/pkg/client"
	"github.com/cedi/urlshortener/pkg/observability"
	redirectpkg "github.com/cedi/urlshortener/pkg/redirect"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
)

var activeRedirects = prometheus.NewGauge(
	prometheus.GaugeOpts{
		Name: "urlshortener_active_redirects",
		Help: "Number of redirects installed for this urlshortener instance",
	},
)

func init() {
	metrics.Registry.MustRegister(activeRedirects)
}

// RedirectReconciler reconciles a Redirect object
type RedirectReconciler struct {
	client  client.Client
	rClient *redirectclient.RedirectClient

	scheme *runtime.Scheme
	log    *logr.Logger
	tracer trace.Tracer

	redirectCount int
	redirects     instrument.Int64UpDownCounter
	latency       instrument.Int64Histogram
}

// NewRedirectReconciler returns a new RedirectReconciler
func NewRedirectReconciler(client client.Client, rClient *redirectclient.RedirectClient, scheme *runtime.Scheme, log *logr.Logger, tracer trace.Tracer, meter metric.Meter) *RedirectReconciler {
	var redirects, _ = meter.Int64UpDownCounter(
		"urlshortener.active_redirects",
		instrument.WithUnit("count"),
		instrument.WithDescription("Amount of redirects (redirect one URL to another)"),
	)

	var redirectReconcileLatency, _ = meter.Int64Histogram(
		"urlshortener.redirect_controller.reconcile_latency",
		instrument.WithUnit("microseconds"),
		instrument.WithDescription("How long does the reconcile function run for"),
	)

	return &RedirectReconciler{
		client:  client,
		rClient: rClient,
		scheme:  scheme,
		log:     log,
		tracer:  tracer,

		redirectCount: 0,
		redirects:     redirects,
		latency:       redirectReconcileLatency,
	}
}

//+kubebuilder:rbac:groups=urlshortener.cedi.dev,resources=redirects,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=urlshortener.cedi.dev,resources=redirects/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=urlshortener.cedi.dev,resources=redirects/finalizers,verbs=update

//+kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses/status,verbs=get;update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.12.1/pkg/reconcile
func (r *RedirectReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	startTime := time.Now()

	defer func() {
		r.latency.Record(ctx, time.Since(startTime).Microseconds(), attribute.String("redirect", req.NamespacedName.String()))
	}()

	log := r.log.WithName("reconciler").WithValues("redirect", req.NamespacedName)

	span := trace.SpanFromContext(ctx)

	// Check if the span was sampled and is recording the data
	if !span.IsRecording() {
		ctx, span = r.tracer.Start(ctx, "RedirectReconciler.Reconcile")
		defer span.End()
	}

	span.SetAttributes(attribute.String("redirect", req.Name))

	// Monitor the number of redirects
	if redirectList, err := r.rClient.List(ctx); redirectList != nil && err == nil {
		activeRedirects.Set(float64(len(redirectList.Items)))

		r.redirects.Add(ctx, int64(len(redirectList.Items)-r.redirectCount))
		r.redirectCount = len(redirectList.Items)
	}

	// get Redirect from etcd
	redirect, err := r.rClient.GetNamespaced(ctx, req.NamespacedName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			observability.RecordInfo(span, &log, "Shortlink resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}

		// Error reading the object - requeue the request.
		observability.RecordError(span, &log, err, "Failed to fetch Redirect resource")
		return ctrl.Result{}, err
	}

	// Check if the ingress already exists, if not create a new one
	ingress, err := r.upsertRedirectIngress(ctx, redirect)
	if err != nil {
		observability.RecordError(span, &log, err, "Failed to upsert redirect ingress")
	}

	// Update the Redirect status with the ingress name and the target
	ingressList := &networkingv1.IngressList{}
	listOpts := []client.ListOption{
		client.InNamespace(redirect.Namespace),
		client.MatchingLabels(redirectpkg.GetLabelsForRedirect(redirect.Name)),
	}

	if err = r.client.List(ctx, ingressList, listOpts...); err != nil {
		observability.RecordError(span, &log, err, "Failed to list ingresses")
		return ctrl.Result{}, err
	}

	// Update status.Nodes if needed
	redirect.Status.IngressName = redirectpkg.GetIngressNames(ingressList.Items)
	redirect.Status.Target = ingress.ObjectMeta.Annotations["nginx.ingress.kubernetes.io/permanent-redirect"]
	err = r.client.Status().Update(ctx, redirect)
	if err != nil {
		observability.RecordError(span, &log, err, "Failed to update Redirect status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *RedirectReconciler) upsertRedirectIngress(ctx context.Context, redirect *urlshortenerv1alpha1.Redirect) (*networkingv1.Ingress, error) {
	ingress := &networkingv1.Ingress{}
	err := r.client.Get(ctx, types.NamespacedName{Name: redirect.Name, Namespace: redirect.Namespace}, ingress)
	ingress = redirectpkg.NewRedirectIngress(ingress, redirect)

	// Set Redirect instance as the owner and controller
	ctrl.SetControllerReference(redirect, ingress, r.scheme)

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
		For(&urlshortenerv1alpha1.Redirect{}).
		Owns(&networkingv1.Ingress{}).
		Complete(r)
}
