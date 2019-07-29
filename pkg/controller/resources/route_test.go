package resources

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	routev1 "github.com/openshift/api/route/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/serving/pkg/apis/networking"
	networkingv1alpha1 "knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
)

func TestMakeRouteWithNoRules(t *testing.T) {
	ci := &networkingv1alpha1.ClusterIngress{
		Spec: networkingv1alpha1.IngressSpec{
			Visibility: networkingv1alpha1.IngressVisibilityExternalIP,
		},
	}

	routes, err := MakeRoutes(ci)
	assert.Zero(t, len(routes))
	assert.Nil(t, err)
}

func TestMakeRouteInternalHost(t *testing.T) {
	ci := &networkingv1alpha1.ClusterIngress{
		Spec: networkingv1alpha1.IngressSpec{
			Visibility: networkingv1alpha1.IngressVisibilityExternalIP,
			Rules: []networkingv1alpha1.IngressRule{{
				Hosts: []string{"test.default.svc.cluster.local"},
			}},
		},
	}

	routes, err := MakeRoutes(ci)
	assert.Zero(t, len(routes))
	assert.Nil(t, err)
}

func TestMakeRouteValidHost(t *testing.T) {
	ci := &networkingv1alpha1.ClusterIngress{
		Spec: networkingv1alpha1.IngressSpec{
			Visibility: networkingv1alpha1.IngressVisibilityExternalIP,
			Rules: []networkingv1alpha1.IngressRule{{
				Hosts: []string{"public.default.domainName"},
			}},
		},
	}

	routes, err := MakeRoutes(ci)
	assert.Zero(t, len(routes))
	assert.Exactly(t, errors.New("Unable to find ClusterIngress LoadBalancer with DomainInternal set"), err)
}

func TestMakeRouteWithEmptyTimeout(t *testing.T) {
	ci := &networkingv1alpha1.ClusterIngress{
		Spec: networkingv1alpha1.IngressSpec{
			Visibility: networkingv1alpha1.IngressVisibilityExternalIP,
			Rules: []networkingv1alpha1.IngressRule{{
				Hosts: []string{"public.default.domainName"},
				HTTP: &networkingv1alpha1.HTTPIngressRuleValue{
					Paths: []networkingv1alpha1.HTTPIngressPath{{}},
				},
			}},
		},
	}

	routes, err := MakeRoutes(ci)
	assert.Zero(t, len(routes))
	assert.Exactly(t, errors.New("Unable to find ClusterIngress LoadBalancer with DomainInternal set"), err)
}

func TestMakeRouteForTimeout(t *testing.T) {
	host := []string{"public.default.domainName", "local.default.domainName"}
	ci := createClusterIngressObj("istio-ingressgateway.istio-system.svc.cluster.local", host)

	routes, _ := MakeRoutes(ci)
	for i, _ := range routes {
		assert.Equal(t, "600s", routes[i].ObjectMeta.Annotations[TimeoutAnnotation])
		assert.NotEqual(t, 10*time.Minute, routes[i].ObjectMeta.Annotations[TimeoutAnnotation])
	}
}

func TestDisableRouteByAnnotation(t *testing.T) {
	host := []string{"public.default.domainName", "local.default.domainName"}
	ci := createClusterIngressObj("istio-ingressgateway.istio-system.svc.cluster.local", host)
	ci.ObjectMeta.Annotations = map[string]string{DisableRoute: ""}

	routes, err := MakeRoutes(ci)
	assert.Equal(t, []*routev1.Route{}, routes)
	assert.Nil(t, err)
}

func TestMakeRouteInvalidDomain(t *testing.T) {
	host := []string{"public.default.domainName"}
	ci := createClusterIngressObj("istio-ingressgateway.istio-system.local.svc.test", host)

	routes, err := MakeRoutes(ci)
	assert.Zero(t, len(routes))
	assert.Exactly(t, errors.New("Unable to find ClusterIngress LoadBalancer with DomainInternal set"), err)
}

func TestMakeRoute(t *testing.T) {
	host := []string{"public.default.domainName", "public.default.svc.local"}
	ci := createClusterIngressObj("istio-ingressgateway.istio-system.svc.cluster.local", host)

	routes, err := MakeRoutes(ci)
	assert.NotNil(t, routes)
	assert.Nil(t, err)

	for i, _ := range routes {
		assert.Equal(t, routes[i].Labels[serving.RouteLabelKey], "route1")
		assert.Equal(t, routes[i].Labels[networking.IngressLabelKey], "clusteringress")

	}
}

func createClusterIngressObj(domainInternal string, host []string) *networkingv1alpha1.ClusterIngress {
	return &networkingv1alpha1.ClusterIngress{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				serving.RouteLabelKey:          "route1",
				serving.RouteNamespaceLabelKey: "default",
			},
			Name: "clusteringress",
		},
		Spec: networkingv1alpha1.IngressSpec{
			Visibility: networkingv1alpha1.IngressVisibilityExternalIP,
			Rules: []networkingv1alpha1.IngressRule{{
				Hosts: host,
				HTTP: &networkingv1alpha1.HTTPIngressRuleValue{
					Paths: []networkingv1alpha1.HTTPIngressPath{{
						Timeout: &metav1.Duration{Duration: 10 * time.Minute},
					}},
				},
			}},
		},
		Status: networkingv1alpha1.IngressStatus{
			LoadBalancer: &networkingv1alpha1.LoadBalancerStatus{
				Ingress: []networkingv1alpha1.LoadBalancerIngressStatus{{
					DomainInternal: domainInternal,
				}},
			},
		},
	}

}
