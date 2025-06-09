package controller

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
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

func init() {
	metrics.Registry.MustRegister(reconcilerDuration)
	metrics.Registry.MustRegister(active)
	metrics.Registry.MustRegister(shortlinkInvocations)
}
