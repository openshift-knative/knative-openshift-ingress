package resources

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"knative.dev/serving/pkg/apis/networking"
	networkingv1alpha1 "knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const namespace = "default"

func TestMakeRouteWithNoRules(t *testing.T) {
	ci := &networkingv1alpha1.ClusterIngress{
		Spec: networkingv1alpha1.IngressSpec{
			Visibility: networkingv1alpha1.IngressVisibilityExternalIP,
		},
	}
	fakeClient := fake.NewFakeClient()

	routes, err := MakeRoutes(ci, fakeClient)
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
	fakeClient := fake.NewFakeClient()

	routes, err := MakeRoutes(ci, fakeClient)
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
	fakeClient := fake.NewFakeClient()

	routes, err := MakeRoutes(ci, fakeClient)
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
	fakeClient := fake.NewFakeClient()

	routes, err := MakeRoutes(ci, fakeClient)
	assert.Zero(t, len(routes))
	assert.Exactly(t, errors.New("Unable to find ClusterIngress LoadBalancer with DomainInternal set"), err)
}

func TestMakeRouteForTimeout(t *testing.T) {
	host := []string{"public.default.domainName", "local.default.domainName"}
	ci := createClusterIngressObj("istio-ingressgateway.istio-system.svc.cluster.local", host)
	fakeClient := fake.NewFakeClient()

	routes, _ := MakeRoutes(ci, fakeClient)
	for i, _ := range routes {
		assert.Equal(t, "600s", routes[i].ObjectMeta.Annotations[TimeoutAnnotation])
		assert.NotEqual(t, 10*time.Minute, routes[i].ObjectMeta.Annotations[TimeoutAnnotation])
	}
}

func TestMakeRouteInvalidDomain(t *testing.T) {
	host := []string{"public.default.domainName"}
	ci := createClusterIngressObj("istio-ingressgateway.istio-system.local.svc.test", host)
	fakeClient := fake.NewFakeClient()

	routes, err := MakeRoutes(ci, fakeClient)
	assert.Zero(t, len(routes))
	assert.Exactly(t, errors.New("Unable to find ClusterIngress LoadBalancer with DomainInternal set"), err)
}

func TestMakeRoute(t *testing.T) {
	host := []string{"public.default.domainName", "public.default.svc.local"}
	ci := createClusterIngressObj("istio-ingressgateway.istio-system.svc.cluster.local", host)
	fakeClient := fake.NewFakeClient()

	routes, err := MakeRoutes(ci, fakeClient)
	assert.NotNil(t, routes)
	assert.Nil(t, err)

	for i, _ := range routes {
		assert.Equal(t, routes[i].Labels[serving.RouteLabelKey], "route1")
		assert.Equal(t, routes[i].Labels[networking.IngressLabelKey], "clusteringress")

	}
}

func TestMakeSecuredRoute(t *testing.T) {
	const testSecretName = "testSecret"
	defaultSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testSecretName,
			Namespace: namespace,
		},
	}

	tests := []struct {
		name           string
		annotations    map[string]string
		want           *routev1.TLSConfig
		wantTargetPort intstr.IntOrString
		wantErr        func(err error) bool
	}{
		{
			name:        "Simple Edge teramination",
			annotations: map[string]string{CertAnnotation: testSecretName, TerminationAnnotation: "edge"},
			want: &routev1.TLSConfig{
				Termination: routev1.TLSTerminationEdge,
			},
			wantTargetPort: intstr.FromInt(80),
			wantErr:        nil,
		},
		{
			name:        "Simple Passthrough teramination",
			annotations: map[string]string{CertAnnotation: testSecretName, TerminationAnnotation: "passthrough"},
			want: &routev1.TLSConfig{
				Termination: routev1.TLSTerminationPassthrough,
			},
			wantTargetPort: intstr.FromInt(443),
			wantErr:        nil,
		},
		{
			name:        "Simple Reencrypt teramination",
			annotations: map[string]string{CertAnnotation: testSecretName, TerminationAnnotation: "reencrypt"},
			want: &routev1.TLSConfig{
				Termination: routev1.TLSTerminationReencrypt,
			},
			wantTargetPort: intstr.FromInt(443),
			wantErr:        nil,
		},
		{
			name:        "Passthrough teramination w/ HttpProtocolAnnotation annotation Redirect",
			annotations: map[string]string{CertAnnotation: testSecretName, TerminationAnnotation: "passthrough", HttpProtocolAnnotation: "Redirect"},
			want: &routev1.TLSConfig{
				Termination:                   routev1.TLSTerminationPassthrough,
				InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyRedirect,
			},
			wantTargetPort: intstr.FromInt(443),
			wantErr:        nil,
		},
		{
			name:        "Passthrough teramination w/ HttpProtocolAnnotation annotation Disable",
			annotations: map[string]string{CertAnnotation: testSecretName, TerminationAnnotation: "passthrough", HttpProtocolAnnotation: "None"},
			want: &routev1.TLSConfig{
				Termination:                   routev1.TLSTerminationPassthrough,
				InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyNone,
			},
			wantTargetPort: intstr.FromInt(443),
			wantErr:        nil,
		},
	}

	for _, test := range tests {
		host := []string{"public.default.domainName", "public.default.svc.local"}
		ci := createClusterIngressObj("istio-ingressgateway.istio-system.svc.cluster.local", host)
		ci.ObjectMeta.Annotations = test.annotations
		fakeClient := fake.NewFakeClient(defaultSecret)
		t.Run(test.name, func(t *testing.T) {

			routes, err := MakeRoutes(ci, fakeClient)
			assert.NotNil(t, routes)
			assert.Nil(t, err)

			for i, _ := range routes {
				assert.Equal(t, test.want, routes[i].Spec.TLS)
				assert.Equal(t, test.wantTargetPort, routes[i].Spec.Port.TargetPort)
			}
		})
	}
}

func createClusterIngressObj(domainInternal string, host []string) *networkingv1alpha1.ClusterIngress {
	return &networkingv1alpha1.ClusterIngress{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				serving.RouteLabelKey:          "route1",
				serving.RouteNamespaceLabelKey: namespace,
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
