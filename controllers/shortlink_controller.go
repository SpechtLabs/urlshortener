package controllers

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/cedi/urlshortener/api/v1alpha1"
	shortlinkclient "github.com/cedi/urlshortener/pkg/client"
	"github.com/spechtlabs/go-otel-utils/otelzap"
)

// ShortLinkReconciler reconciles a ShortLink object
type ShortLinkReconciler struct {
	client *shortlinkclient.ShortlinkClient
	scheme *runtime.Scheme
	tracer trace.Tracer
}

// NewShortLinkReconciler returns a new ShortLinkReconciler
func NewShortLinkReconciler(client *shortlinkclient.ShortlinkClient, scheme *runtime.Scheme, tracer trace.Tracer) *ShortLinkReconciler {
	return &ShortLinkReconciler{
		client: client,
		scheme: scheme,
		tracer: tracer,
	}
}

//+kubebuilder:rbac:groups=urlshortener.cedi.dev,resources=shortlinks,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=urlshortener.cedi.dev,resources=shortlinks/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=urlshortener.cedi.dev,resources=shortlinks/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.12.1/pkg/reconcile
func (r *ShortLinkReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	startTime := time.Now()
	defer func() {
		reconcilerDuration.WithLabelValues("shortlink", req.Name, req.Namespace).Observe(float64(time.Since(startTime).Microseconds()))
	}()

	span := trace.SpanFromContext(ctx)

	// Check if the span was sampled and is recording the data
	if !span.IsRecording() {
		ctx, span = r.tracer.Start(ctx, "ShortLinkReconciler.Reconcile")
		defer span.End()
	}

	span.SetAttributes(attribute.String("shortlink", req.Name))

	// Get ShortLink from etcd
	shortlink, err := r.client.GetNamespaced(ctx, req.NamespacedName)
	if err != nil || shortlink == nil {
		if errors.IsNotFound(err) {
			otelzap.L().WithError(err).Ctx(ctx).Info("Shortlink resource not found. Ignoring since object must be deleted",
				zap.String("name", "reconciler"),
				zap.String("shortlink", req.String()),
			)
		} else {
			otelzap.L().WithError(err).Ctx(ctx).Error("Failed to fetch ShortLink resource",
				zap.String("name", "reconciler"),
				zap.String("shortlink", req.String()),
			)
		}
	}

	if shortlinkList, err := r.client.ListNamespaced(ctx, req.Namespace); shortlinkList != nil && err == nil {
		active.WithLabelValues("shortlink").Set(float64(len(shortlinkList.Items)))

		for _, shortlink := range shortlinkList.Items {
			shortlinkInvocations.WithLabelValues(
				shortlink.Name,
				shortlink.Namespace,
			).Set(float64(shortlink.Status.Count))
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the api with the Manager.
func (r *ShortLinkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.ShortLink{}).
		Complete(r)
}
