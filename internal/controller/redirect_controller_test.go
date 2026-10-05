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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	urlshortenerv1alpha1 "github.com/spechtlabs/urlshortener/api/v1alpha1"
)

var _ = Describe("Redirect Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-redirect"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		reconcileRedirect := func() {
			GinkgoHelper()

			controllerReconciler := NewRedirectReconciler(k8sClient, k8sClient.Scheme())
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
		}

		BeforeEach(func() {
			By("creating the custom resource for the Kind Redirect")
			resource := &urlshortenerv1alpha1.Redirect{
				Name:      resourceName,
				Namespace: "default",
				Spec: urlshortenerv1alpha1.RedirectSpec{
					Source: "old.example.com",
					Target: "new.example.com",
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			By("Cleanup the specific resource instance Redirect")
			resource := &urlshortenerv1alpha1.Redirect{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

			// envtest runs no garbage collector, so the Ingress the Redirect
			// owns is deleted by hand.
			ingress := &networkingv1.Ingress{}
			if err := k8sClient.Get(ctx, typeNamespacedName, ingress); err == nil {
				Expect(k8sClient.Delete(ctx, ingress)).To(Succeed())
			}
		})

		It("should create an Ingress that redirects to the target", func() {
			reconcileRedirect()

			ingress := &networkingv1.Ingress{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, ingress)).To(Succeed())
			Expect(ingress.Labels).To(Equal(GetLabelsForRedirect(resourceName)))
			Expect(ingress.Annotations).To(HaveKeyWithValue(permanentRedirectAnnotation, "http://new.example.com$request_uri"))
			Expect(ingress.Annotations).To(HaveKeyWithValue("nginx.ingress.kubernetes.io/permanent-redirect-code", "308"))
			Expect(ingress.Spec.Rules).To(HaveLen(1))
			Expect(ingress.Spec.Rules[0].Host).To(Equal("old.example.com"))
			Expect(ingress.Spec.TLS).To(BeEmpty())
			Expect(ingress.OwnerReferences).To(HaveLen(1))
			Expect(ingress.OwnerReferences[0].Name).To(Equal(resourceName))

			redirect := &urlshortenerv1alpha1.Redirect{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, redirect)).To(Succeed())
			Expect(redirect.Status.Target).To(Equal("http://new.example.com$request_uri"))
			Expect(redirect.Status.IngressName).To(Equal([]string{resourceName}))
		})

		It("should update the Ingress when the Redirect changes", func() {
			reconcileRedirect()

			redirect := &urlshortenerv1alpha1.Redirect{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, redirect)).To(Succeed())
			redirect.Spec.Target = "https://newer.example.com"
			redirect.Spec.Code = 301
			redirect.Spec.TLS = urlshortenerv1alpha1.TLSSpec{
				Enable:      true,
				Annotations: map[string]string{"cert-manager.io/cluster-issuer": "letsencrypt"},
			}
			Expect(k8sClient.Update(ctx, redirect)).To(Succeed())

			reconcileRedirect()

			ingress := &networkingv1.Ingress{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, ingress)).To(Succeed())
			Expect(ingress.Annotations).To(HaveKeyWithValue(permanentRedirectAnnotation, "https://newer.example.com"))
			Expect(ingress.Annotations).To(HaveKeyWithValue("nginx.ingress.kubernetes.io/permanent-redirect-code", "301"))
			Expect(ingress.Annotations).To(HaveKeyWithValue("cert-manager.io/cluster-issuer", "letsencrypt"))
			Expect(ingress.Spec.TLS).To(HaveLen(1))
			Expect(ingress.Spec.TLS[0].SecretName).To(Equal("old-example-com-redirect-secret"))
		})

		It("should fail when another controller owns the Ingress", func() {
			other := &urlshortenerv1alpha1.Redirect{
				Name: "other-redirect", Namespace: "default",
				Spec: urlshortenerv1alpha1.RedirectSpec{Source: "other.example.com", Target: "example.com"},
			}
			Expect(k8sClient.Create(ctx, other)).To(Succeed())
			DeferCleanup(func() { Expect(k8sClient.Delete(ctx, other)).To(Succeed()) })

			ingress := &networkingv1.Ingress{Name: resourceName, Namespace: "default"}
			Expect(UpdateRedirectIngress(ingress, other, k8sClient.Scheme())).To(Succeed())
			ingress.Name = resourceName
			Expect(k8sClient.Create(ctx, ingress)).To(Succeed())

			controllerReconciler := NewRedirectReconciler(k8sClient, k8sClient.Scheme())
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).To(HaveOccurred())
		})

		It("should ignore a redirect that was deleted", func() {
			controllerReconciler := NewRedirectReconciler(k8sClient, k8sClient.Scheme())

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				Name: "deleted", Namespace: "default",
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
