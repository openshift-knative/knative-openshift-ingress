package resources

import (
	"github.com/stretchr/testify/assert"
	"testing"

	networkingv1alpha1 "github.com/knative/serving/pkg/apis/networking/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMakeRoute(t *testing.T) {
	ci := &networkingv1alpha1.ClusterIngress{
		Spec: networkingv1alpha1.IngressSpec{
			Visibility: networkingv1alpha1.IngressVisibilityExternalIP,
			Rules: []networkingv1alpha1.ClusterIngressRule{{
				Hosts: []string{"public.default.domainName", "local.default.domainName"},
				HTTP: &networkingv1alpha1.HTTPClusterIngressRuleValue{
					Paths: []networkingv1alpha1.HTTPClusterIngressPath{{
						Timeout: &metav1.Duration{Duration: networkingv1alpha1.DefaultTimeout},
					}},
				},
			}},
		},
		Status: networkingv1alpha1.IngressStatus{
			LoadBalancer: &networkingv1alpha1.LoadBalancerStatus{
				Ingress: []networkingv1alpha1.LoadBalancerIngressStatus{{
					DomainInternal: "istio-ingressgateway.istio-system.svc.cluster.local",
				}},
			},
		},
	}

	routes, _ := MakeRoutes(ci)
	for i, _ := range routes {
		assert.Equal(t, "600s", routes[i].ObjectMeta.Annotations[timeoutAnnotation])
		assert.NotEqual(t, networkingv1alpha1.DefaultTimeout, routes[i].ObjectMeta.Annotations[timeoutAnnotation])
	}
}
