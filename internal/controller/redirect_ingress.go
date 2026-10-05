package controller

import (
	"fmt"
	"maps"
	"regexp"
	"strings"

	"github.com/sierrasoftworks/humane-errors-go"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"

	networkingv1 "k8s.io/api/networking/v1"

	"github.com/spechtlabs/urlshortener/api/v1alpha1"
)

// permanentRedirectAnnotation is the ingress-nginx annotation that carries
// the redirect target; the Redirect's status reports it back.
const permanentRedirectAnnotation = "nginx.ingress.kubernetes.io/permanent-redirect"

// protocolPattern matches a URL that names its protocol (`scheme://...`).
var protocolPattern = regexp.MustCompile(`^(.+)(:\/\/).*$`)

// UpdateRedirectIngress sets the labels, annotations, rules and owner of ing,
// which has the Redirect's name and namespace, to what redirect asks for. It
// leaves the rest of ing's metadata alone, so it works on a new Ingress as
// well as on one read from the cluster.
func UpdateRedirectIngress(ing *networkingv1.Ingress, redirect *v1alpha1.Redirect, scheme *runtime.Scheme) humane.Error {
	pathTypePrefix := networkingv1.PathTypePrefix

	ing.Labels = GetLabelsForRedirect(redirect.Name)
	ing.Annotations = map[string]string{
		"nginx.ingress.kubernetes.io/rewrite-target":          "/",
		permanentRedirectAnnotation:                           normalizeURL(redirect.Spec.Target),
		"nginx.ingress.kubernetes.io/permanent-redirect-code": fmt.Sprintf("%d", redirect.Spec.Code),
	}

	ing.Spec = networkingv1.IngressSpec{
		IngressClassName: &redirect.Spec.IngressClassName,
		Rules: []networkingv1.IngressRule{
			{
				Host: redirect.Spec.Source,
				HTTP: &networkingv1.HTTPIngressRuleValue{
					Paths: []networkingv1.HTTPIngressPath{
						{
							Path:     "/",
							PathType: &pathTypePrefix,
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: "http-svc",
									Port: networkingv1.ServiceBackendPort{
										Number: 80,
									},
								},
							},
						},
					},
				},
			},
		},
	}

	if redirect.Spec.TLS.Enable {
		ing.Spec.TLS = []networkingv1.IngressTLS{
			{
				Hosts:      []string{redirect.Spec.Source},
				SecretName: fmt.Sprintf("%s-redirect-secret", strings.ReplaceAll(redirect.Spec.Source, ".", "-")),
			},
		}

		// Add additional annotations based from our TLS spec
		maps.Copy(ing.Annotations, redirect.Spec.TLS.Annotations)
	}

	// Set Redirect instance as the owner, so the Ingress is deleted with it
	if err := ctrl.SetControllerReference(redirect, ing, scheme); err != nil {
		return humane.Wrap(err, fmt.Sprintf("Failed to make Redirect %s the owner of its Ingress", redirect.Name),
			"Another controller owns an Ingress of the same name; rename the Redirect or delete that Ingress",
		)
	}

	return nil
}

// GetLabelsForRedirect returns the labels for selecting the resources
// belonging to the given redirect CRD name.
func GetLabelsForRedirect(name string) map[string]string {
	return map[string]string{"app": "urlshortener", "redirect": name}
}

// GetIngressNames returns a []string from a []networkingv1.Ingress object
// containing only the networkingv1.Ingress.ObjectMeta.Name of the input
func GetIngressNames(ingresses []networkingv1.Ingress) []string {
	ingressNames := make([]string, 0, len(ingresses))

	for _, ingress := range ingresses {
		ingressNames = append(ingressNames, ingress.Name)
	}

	return ingressNames
}

// normalizeURL prepends http:// to a target that doesn't name its protocol,
// and keeps the request URI, so example.com redirects /foo to
// http://example.com/foo.
func normalizeURL(redirectTarget string) string {
	if !protocolPattern.MatchString(redirectTarget) {
		redirectTarget = fmt.Sprintf("http://%s$request_uri", redirectTarget)
	}

	return redirectTarget
}
