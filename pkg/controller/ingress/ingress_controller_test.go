package ingress

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/openshift-knative/knative-openshift-ingress/pkg/controller/common"
	"github.com/openshift-knative/knative-openshift-ingress/pkg/controller/resources"

	routev1 "github.com/openshift/api/route/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	networkingv1alpha1 "knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

const (
	name       = "ingress-operator"
	namespace  = "istio-system"
	domainName = name + "." + namespace + ".default.domainName"
)

var (
	defaultIngress = &networkingv1alpha1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
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
)

func TestRouteMigration(t *testing.T) {
	logf.SetLogger(logf.ZapLogger(true))

	tests := []struct {
		name        string
		exRoute     *routev1.Route
		wantName    string
		removedName string
	}{
		{
			name: "Clean up old route and new route is generated",
			exRoute: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{Name: "test-0"},
				Spec:       routev1.RouteSpec{Host: domainName},
			},
			wantName:    domainName,
			removedName: "test-0",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Register operator types with the runtime scheme.
			s := scheme.Scheme
			s.AddKnownTypes(networkingv1alpha1.SchemeGroupVersion, defaultIngress)
			s.AddKnownTypes(routev1.SchemeGroupVersion, test.exRoute)
			s.AddKnownTypes(routev1.SchemeGroupVersion, &routev1.RouteList{})
			// Create a fake client to mock API calls.
			cl := fake.NewFakeClient(defaultIngress, test.exRoute)
			// Create a Reconcile Ingress object with the scheme and fake client.
			r := &ReconcileIngress{base: &common.BaseIngressReconciler{Client: cl}, client: cl, scheme: s}
			// Mock request to simulate Reconcile() being called on an event for a
			// watched resource .
			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      name,
					Namespace: namespace,
				},
			}
			if _, err := r.Reconcile(req); err != nil {
				t.Fatalf("reconcile: (%v)", err)
			}

			// Check if ex-Route has been reconciled with new name.
			route := &routev1.Route{}
			err := cl.Get(context.TODO(), types.NamespacedName{Name: test.wantName, Namespace: namespace}, route)
			assert.Nil(t, err)
			assert.Equal(t, test.exRoute.Spec.Host, route.Spec.Host)

			// Check if ex-Route has been deleted.
			err = cl.Get(context.TODO(), types.NamespacedName{Name: test.removedName, Namespace: namespace}, route)
			assert.True(t, errors.IsNotFound(err))

		})
	}
}

// TestIngressController runs Reconcile ReconcileIngress.Reconcile() against a
// fake client that tracks an Ingress object.
func TestIngressController(t *testing.T) {
	logf.SetLogger(logf.ZapLogger(true))

	tests := []struct {
		name        string
		annotations map[string]string
		want        map[string]string
		wantErr     func(err error) bool
	}{
		{
			name:        "reconcile route with timeout annotation",
			annotations: map[string]string{},
			want:        map[string]string{resources.TimeoutAnnotation: "5s"},
			wantErr:     func(err error) bool { return err == nil },
		},
		{
			name:        "reconcile route with taking over annotations",
			annotations: map[string]string{serving.CreatorAnnotation: "userA", serving.UpdaterAnnotation: "userB"},
			want:        map[string]string{serving.CreatorAnnotation: "userA", serving.UpdaterAnnotation: "userB", resources.TimeoutAnnotation: "5s"},
			wantErr:     func(err error) bool { return err == nil },
		},
		{
			name:        "do not reconcile with disable route annotation",
			annotations: map[string]string{resources.DisableRouteAnnotation: ""},
			want:        nil,
			wantErr:     errors.IsNotFound,
		},
		{
			name:        "reconcile route with passthrough annotation",
			annotations: map[string]string{resources.TLSTerminationAnnotation: "passthrough"},
			want:        map[string]string{resources.TLSTerminationAnnotation: "passthrough", resources.TimeoutAnnotation: "5s"},
			wantErr:     func(err error) bool { return err == nil },
		},
		{
			name:        "reconcile route with invalid TLS termination annotation",
			annotations: map[string]string{resources.TLSTerminationAnnotation: "edge"},
			want:        nil,
			wantErr:     errors.IsNotFound,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			// Set test annotation
			defaultIngress.SetAnnotations(test.annotations)

			// route object
			route := &routev1.Route{}

			// Register operator types with the runtime scheme.
			s := scheme.Scheme
			s.AddKnownTypes(networkingv1alpha1.SchemeGroupVersion, defaultIngress)
			s.AddKnownTypes(routev1.SchemeGroupVersion, route)
			s.AddKnownTypes(routev1.SchemeGroupVersion, &routev1.RouteList{})
			// Create a fake client to mock API calls.
			cl := fake.NewFakeClient(defaultIngress, route)
			// Create a Reconcile Ingress object with the scheme and fake client.
			r := &ReconcileIngress{base: &common.BaseIngressReconciler{Client: cl}, client: cl, scheme: s}

			// Mock request to simulate Reconcile() being called on an event for a
			// watched resource .
			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      name,
					Namespace: namespace,
				},
			}
			if _, err := r.Reconcile(req); err != nil {
				t.Fatalf("reconcile: (%v)", err)
			}

			// Check if route has been created
			routes := &routev1.Route{}
			err := cl.Get(context.TODO(), types.NamespacedName{Name: domainName, Namespace: namespace}, routes)

			assert.True(t, test.wantErr(err))
			assert.Equal(t, test.want, routes.ObjectMeta.Annotations)
		})
	}
}
