package clusteringress

import (
	"context"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"

	"github.com/openshift-knative/knative-openshift-ingress/pkg/controller/clusteringress/resources"

	networkingv1alpha1 "github.com/knative/serving/pkg/apis/networking/v1alpha1"
	routev1 "github.com/openshift/api/route/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

// TestClusterIngressController runs Reconcile ReconcileClusterIngress.Reconcile() against a
// fake client that tracks a ClusteIngress object.
func TestClusterIngressController(t *testing.T) {
	logf.SetLogger(logf.ZapLogger(true))

	var (
		name      = "clusteringress-operator"
		namespace = "istio-system"
	)

	// A ClusterIngress resource with metadata and spec.
	clusteringress := &networkingv1alpha1.ClusterIngress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: networkingv1alpha1.IngressSpec{
			Visibility: networkingv1alpha1.IngressVisibilityExternalIP,
			Rules: []networkingv1alpha1.ClusterIngressRule{{
				Hosts: []string{"public.default.domainName"},
				HTTP: &networkingv1alpha1.HTTPClusterIngressRuleValue{
					Paths: []networkingv1alpha1.HTTPClusterIngressPath{{
						Timeout: &metav1.Duration{Duration: 5 * time.Second},
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

	// route object
	route := &routev1.Route{}

	// Objects to track in the fake client.
	objs := []runtime.Object{
		clusteringress,
		route,
	}

	// Register operator types with the runtime scheme.
	s := scheme.Scheme
	s.AddKnownTypes(networkingv1alpha1.SchemeGroupVersion, clusteringress)
	s.AddKnownTypes(routev1.SchemeGroupVersion, route)
	// Create a fake client to mock API calls.
	cl := fake.NewFakeClient(objs...)
	// Create a Reconcile ClusterIngress object with the scheme and fake client.
	r := &ReconcileClusterIngress{client: cl, scheme: s}

	// Mock request to simulate Reconcile() being called on an event for a
	// watched resource .
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      name,
			Namespace: "istio-system",
		},
	}
	_, err := r.Reconcile(req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	// Check if route has been created
	routes := &routev1.Route{}

	err = cl.Get(context.TODO(), types.NamespacedName{Name: "clusteringress-operator-0", Namespace: "istio-system"}, routes)
	if err != nil {
		t.Fatalf("get route: (%v)", err)
	}

	assert.Equal(t, "5s", routes.ObjectMeta.Annotations[resources.TimeoutAnnotation])
	assert.NotEqual(t, networkingv1alpha1.DefaultTimeout, routes.ObjectMeta.Annotations[resources.TimeoutAnnotation])

}
