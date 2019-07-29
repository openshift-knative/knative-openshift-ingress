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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	networkingv1alpha1 "knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var (
	name      = "ingress-operator"
	namespace = "istio-system"

	defaultIngress = &networkingv1alpha1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: networkingv1alpha1.IngressSpec{
			Visibility: networkingv1alpha1.IngressVisibilityExternalIP,
			Rules: []networkingv1alpha1.IngressRule{{
				Hosts: []string{"public.default.domainName"},
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
			name:        "reconcile route with ingress annotation",
			annotations: map[string]string{serving.CreatorAnnotation: "userA", serving.UpdaterAnnotation: "userB"},
			want:        map[string]string{serving.CreatorAnnotation: "userA", serving.UpdaterAnnotation: "userB", resources.TimeoutAnnotation: "5s"},
			wantErr:     func(err error) bool { return err == nil },
		},
		{
			name:        "do not reconcile with disable route annotation",
			annotations: map[string]string{resources.DisableRoute: ""},
			want:        nil,
			wantErr:     errors.IsNotFound,
		},
	}

	for _, test := range tests {

		// An Ingress resource with metadata and spec.
		ingress := defaultIngress
		// Set test annotation
		ingress.SetAnnotations(test.annotations)

		// route object
		route := &routev1.Route{}

		// Objects to track in the fake client.
		objs := []runtime.Object{
			ingress,
			route,
		}

		// Register operator types with the runtime scheme.
		s := scheme.Scheme
		s.AddKnownTypes(networkingv1alpha1.SchemeGroupVersion, ingress)
		s.AddKnownTypes(routev1.SchemeGroupVersion, route)
		// Create a fake client to mock API calls.
		cl := fake.NewFakeClient(objs...)
		// Create a Reconcile Ingress object with the scheme and fake client.
		r := &ReconcileIngress{base: &common.BaseIngressReconciler{Client: cl}, client: cl, scheme: s}

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
		err = cl.Get(context.TODO(), types.NamespacedName{Name: "ingress-operator-0", Namespace: "istio-system"}, routes)

		assert.True(t, test.wantErr(err))
		assert.Equal(t, routes.ObjectMeta.Annotations, test.want)
	}
}
