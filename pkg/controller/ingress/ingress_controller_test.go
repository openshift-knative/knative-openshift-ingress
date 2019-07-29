package ingress

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/openshift-knative/knative-openshift-ingress/pkg/controller/common"
	"github.com/openshift-knative/knative-openshift-ingress/pkg/controller/resources"

	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
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
	name           = "ingress-operator"
	namespace      = "istio-system"
	testSecretName = "testSecret"
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
	defaultSecret = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testSecretName,
			Namespace: namespace,
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
			name:        "reconcile route with secured route annotations",
			annotations: map[string]string{resources.CertAnnotation: testSecretName, resources.TerminationAnnotation: "edge", resources.HttpProtocolAnnotation: "Disabled"},
			want:        map[string]string{resources.CertAnnotation: testSecretName, resources.TerminationAnnotation: "edge", resources.HttpProtocolAnnotation: "Disabled", resources.TimeoutAnnotation: "5s"},
			wantErr:     func(err error) bool { return err == nil },
		},
		{
			name:        "reconcile route with ingress annotation",
			annotations: map[string]string{serving.CreatorAnnotation: "userA", serving.UpdaterAnnotation: "userB"},
			want:        map[string]string{serving.CreatorAnnotation: "userA", serving.UpdaterAnnotation: "userB", resources.TimeoutAnnotation: "5s"},
			wantErr:     func(err error) bool { return err == nil },
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
			// Create a fake client to mock API calls.
			cl := fake.NewFakeClient(defaultIngress, route, defaultSecret)
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
			if _, err := r.Reconcile(req); err != nil {
				t.Fatalf("reconcile: (%v)", err)
			}

			// Check if route has been created
			routes := &routev1.Route{}
			err := cl.Get(context.TODO(), types.NamespacedName{Name: "ingress-operator-0", Namespace: "istio-system"}, routes)

			assert.True(t, test.wantErr(err))
			assert.Equal(t, test.want, routes.ObjectMeta.Annotations)
		})
	}
}
