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

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/spechtlabs/go-otel-utils/otelzap"

	urlshortenerv1alpha1 "github.com/spechtlabs/urlshortener/api/v1alpha1"
	shortlinkclient "github.com/spechtlabs/urlshortener/pkg/client"
)

// ShortlinkReconciler reconciles a Shortlink object
type ShortlinkReconciler struct {
	client *shortlinkclient.ShortlinkClient
	scheme *runtime.Scheme
}

// NewShortLinkReconciler returns a new ShortLinkReconciler
func NewShortLinkReconciler(client client.Client, scheme *runtime.Scheme) *ShortlinkReconciler {
	return &ShortlinkReconciler{
		client: shortlinkclient.NewShortlinkClient(client),
		scheme: scheme,
	}
}

// +kubebuilder:rbac:groups=urlshortener.cedi.dev,resources=shortlinks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=urlshortener.cedi.dev,resources=shortlinks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=urlshortener.cedi.dev,resources=shortlinks/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// the Shortlink object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.4/pkg/reconcile
func (r *ShortlinkReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	span := trace.SpanFromContext(ctx)

	startTime := time.Now()
	defer func() {
		reconcilerDuration.WithLabelValues("shortlink", req.Name, req.Namespace).Observe(float64(time.Since(startTime).Microseconds()))
	}()

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

// SetupWithManager sets up the controller with the Manager.
func (r *ShortlinkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&urlshortenerv1alpha1.Shortlink{}).
		Named("shortlink").
		Complete(r)
}
