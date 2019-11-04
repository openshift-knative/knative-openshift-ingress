package clusteringress

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/openshift-knative/knative-openshift-ingress/pkg/controller/common"
	"github.com/openshift-knative/knative-openshift-ingress/pkg/controller/resources"

	routev1 "github.com/openshift/api/route/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	networkingv1alpha1 "knative.dev/serving/pkg/apis/networking/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

// TestClusterIngressController runs Reconcile ReconcileClusterIngress.Reconcile() against a
// fake client that tracks a ClusteIngress object.
func TestClusterIngressController(t *testing.T) {
	logf.SetLogger(logf.ZapLogger(true))

	var (
		name       = "clusteringress-operator"
		namespace  = "istio-system"
		uid        = "8a7e9a9d-fbc6-11e9-a88e-0261aff8d6d8"
		domainName = name + "." + namespace + ".default.domainName"
		routeName0 = "route-" + uid + "-0"
	)

	// A ClusterIngress resource with metadata and spec.
	clusteringress := &networkingv1alpha1.ClusterIngress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       types.UID(uid),
		},
		Spec: networkingv1alpha1.IngressSpec{
			Visibility: networkingv1alpha1.IngressVisibilityExternalIP,
			Rules: []networkingv1alpha1.IngressRule{{
				Hosts: []string{domainName},
				HTTP: &networkingv1alpha1.HTTPIngressRuleValue{
					Paths: []networkingv1alpha1.HTTPIngressPath{{
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
	s.AddKnownTypes(routev1.SchemeGroupVersion, &routev1.RouteList{})
	// Create a fake client to mock API calls.
	cl := fake.NewFakeClient(objs...)
	// Create a Reconcile ClusterIngress object with the scheme and fake client.
	r := &ReconcileClusterIngress{base: &common.BaseIngressReconciler{Client: cl}, client: cl, scheme: s}

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

	err = cl.Get(context.TODO(), types.NamespacedName{Name: routeName0, Namespace: "istio-system"}, routes)
	if err != nil {
		t.Fatalf("get route: (%v)", err)
	}

	assert.Equal(t, "5s", routes.ObjectMeta.Annotations[resources.TimeoutAnnotation])
	assert.NotEqual(t, 10*time.Minute, routes.ObjectMeta.Annotations[resources.TimeoutAnnotation])

}
