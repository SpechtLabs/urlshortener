package controller

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sierrasoftworks/humane-errors-go"
	ctrl "sigs.k8s.io/controller-runtime"
)

var reconcilerDuration = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Name: "urlshortener_reconciler_duration",
		Help: "How long the reconcile loop ran for in microseconds",
	},
	[]string{
		"reconciler",
		"name",
		"namespace",
	},
)

var active = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "urlshortener_active",
		Help: "Number of installed urlshortener objects for this instance",
	},
	[]string{
		"type",
	},
)

var shortlinkInvocations = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "urlshortener_shortlink_invocation",
		Help: "Counts of how often a shortlink was invoked",
	},
	[]string{
		"name",
		"namespace",
	},
)

// RegisterMetrics registers the reconcilers' metrics with registerer, which is
// controller-runtime's metrics.Registry in the manager, so they're served on
// its metrics endpoint. Call it once, before the manager starts.
func RegisterMetrics(registerer prometheus.Registerer) humane.Error {
	for _, collector := range []prometheus.Collector{reconcilerDuration, active, shortlinkInvocations} {
		if err := registerer.Register(collector); err != nil {
			return humane.Wrap(err, "Failed to register the reconciler metrics",
				"RegisterMetrics registers each metric once; make sure it is called only once per registry",
			)
		}
	}

	return nil
}

// timeReconcile starts timing a reconcile of req by reconciler; the
// ObserveDuration of the timer it returns records the time in
// reconcilerDuration, in microseconds.
func timeReconcile(reconciler string, req ctrl.Request) *prometheus.Timer {
	observer := reconcilerDuration.WithLabelValues(reconciler, req.Name, req.Namespace)

	return prometheus.NewTimer(prometheus.ObserverFunc(func(seconds float64) {
		observer.Observe(seconds * float64(time.Second/time.Microsecond))
	}))
}
