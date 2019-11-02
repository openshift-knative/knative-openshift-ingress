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
	"knative.dev/serving/pkg/apis/networking"
	networkingv1alpha1 "knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

const (
	name       = "ingress-operator"
	namespace  = "istio-system"
	uid        = "8a7e9a9d-fbc6-11e9-a88e-0261aff8d6d8"
	domainName = name + "." + namespace + ".default.domainName"
	routeName0 = "route-" + uid + "-0"
)

var (
	defaultIngress = &networkingv1alpha1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       uid,
			Labels:    map[string]string{serving.RouteNamespaceLabelKey: namespace, serving.RouteLabelKey: name},
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

	test := struct {
		name            string
		exRoute         *routev1.Route
		noLabelRoute    *routev1.Route
		otherLabelRoute *routev1.Route
		wantName        string
	}{
		name: "Clean up old route and new route is generated",
		exRoute: &routev1.Route{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "test-0",
				Labels: map[string]string{networking.IngressLabelKey: name, serving.RouteLabelKey: name, serving.RouteNamespaceLabelKey: namespace},
			},
			Spec: routev1.RouteSpec{Host: domainName},
		},
		noLabelRoute: &routev1.Route{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "no-remove-1",
				Labels: map[string]string{networking.IngressLabelKey: name, serving.RouteLabelKey: name},
			},
			Spec: routev1.RouteSpec{Host: domainName},
		},
		otherLabelRoute: &routev1.Route{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "no-remove-2",
				Labels: map[string]string{networking.IngressLabelKey: "another", serving.RouteLabelKey: name, serving.RouteNamespaceLabelKey: namespace},
			},
			Spec: routev1.RouteSpec{Host: domainName},
		},
		wantName: routeName0,
	}

	t.Run(test.name, func(t *testing.T) {
		// Register operator types with the runtime scheme.
		s := scheme.Scheme
		s.AddKnownTypes(networkingv1alpha1.SchemeGroupVersion, defaultIngress)
		s.AddKnownTypes(routev1.SchemeGroupVersion, test.exRoute)
		s.AddKnownTypes(routev1.SchemeGroupVersion, &routev1.RouteList{})

		// Create a fake client to mock API calls.
		cl := fake.NewFakeClient(defaultIngress, test.exRoute, test.noLabelRoute, test.otherLabelRoute)

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

		route := &routev1.Route{}
		// Check if ex-Route has been reconciled with new name.
		err := cl.Get(context.TODO(), types.NamespacedName{Name: test.wantName}, route)
		assert.Nil(t, err)
		// Check if new Route has same spec.Host with ex-Route's
		assert.Equal(t, test.exRoute.Spec.Host, route.Spec.Host)

		// Check if ex-Route has been deleted.
		err = cl.Get(context.TODO(), types.NamespacedName{Name: test.exRoute.Name}, route)
		assert.True(t, errors.IsNotFound(err))

		// Check if noLabelRoute has NOT been deleted.
		err = cl.Get(context.TODO(), types.NamespacedName{Name: test.noLabelRoute.Name}, route)
		assert.Nil(t, err)
		assert.Equal(t, test.noLabelRoute.Spec.Host, route.Spec.Host)

		// Check if otherLabelRoute has NOT been deleted.
		err = cl.Get(context.TODO(), types.NamespacedName{Name: test.otherLabelRoute.Name}, route)
		assert.Nil(t, err)
		assert.Equal(t, test.otherLabelRoute.Spec.Host, route.Spec.Host)
	})
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
			err := cl.Get(context.TODO(), types.NamespacedName{Name: routeName0, Namespace: namespace}, routes)

			assert.True(t, test.wantErr(err))
			assert.Equal(t, test.want, routes.ObjectMeta.Annotations)
		})
	}
}
