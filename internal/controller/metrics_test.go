package controller

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	networkingv1 "k8s.io/api/networking/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

func TestRegisterMetrics(t *testing.T) {
	registry := prometheus.NewRegistry()

	if err := RegisterMetrics(registry); err != nil {
		t.Fatalf("RegisterMetrics() error = %v", err)
	}

	if err := RegisterMetrics(registry); err == nil {
		t.Errorf("RegisterMetrics() twice on one registry = nil, want an error")
	}

	timeReconcile("redirect", ctrl.Request{}).ObserveDuration()

	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}

	for _, family := range families {
		if family.GetName() == "urlshortener_reconciler_duration" {
			return
		}
	}

	t.Errorf("urlshortener_reconciler_duration wasn't gathered")
}

func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		target string
		want   string
	}{
		{target: "https://example.com/path", want: "https://example.com/path"},
		{target: "ftp://example.com", want: "ftp://example.com"},
		{target: "example.com", want: "http://example.com$request_uri"},
	}

	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			if got := normalizeURL(tt.target); got != tt.want {
				t.Errorf("normalizeURL(%q) = %q, want %q", tt.target, got, tt.want)
			}
		})
	}
}

func TestGetIngressNames(t *testing.T) {
	tests := []struct {
		name      string
		ingresses []networkingv1.Ingress
		want      []string
	}{
		{name: "none", want: []string{}},
		{
			name: "several",
			ingresses: []networkingv1.Ingress{
				{Name: "a"},
				{Name: "b"},
			},
			want: []string{"a", "b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetIngressNames(tt.ingresses)
			if len(got) != len(tt.want) {
				t.Fatalf("GetIngressNames() = %v, want %v", got, tt.want)
			}

			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("GetIngressNames() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}
