package ingress

import (
	"context"
	"testing"
	"time"

	maistrav1 "github.com/maistra/istio-operator/pkg/apis/maistra/v1"
	"github.com/openshift-knative/knative-openshift-ingress/pkg/controller/common"
	"github.com/openshift-knative/knative-openshift-ingress/pkg/controller/resources"
	routev1 "github.com/openshift/api/route/v1"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/scheme"
	"knative.dev/pkg/kmeta"
	"knative.dev/serving/pkg/apis/networking"
	networkingv1alpha1 "knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/network"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

const (
	name                 = "ingress-operator"
	serviceMeshNamespace = "knative-serving-ingress"
	smmrName             = "default"
	namespace            = "ingress-namespace"
	uid                  = "8a7e9a9d-fbc6-11e9-a88e-0261aff8d6d8"
	domainName           = name + "." + namespace + ".default.domainName"
	routeName0           = "route-" + uid + "-336636653035"
)

var (
	defaultIngress = &networkingv1alpha1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			UID:         uid,
			Labels:      map[string]string{serving.RouteNamespaceLabelKey: namespace, serving.RouteLabelKey: name},
			Annotations: map[string]string{networking.IngressClassAnnotationKey: network.IstioIngressClassName},
			Finalizers:  []string{"ingresses"},
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
					DomainInternal: "istio-ingressgateway." + serviceMeshNamespace + ".svc.cluster.local",
				}},
			},
		},
	}

	defaultIngressForClusterLocal = &networkingv1alpha1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			UID:         uid,
			Labels:      map[string]string{serving.RouteNamespaceLabelKey: namespace, serving.RouteLabelKey: name},
			Annotations: map[string]string{networking.IngressClassAnnotationKey: network.IstioIngressClassName},
		},
		Spec: networkingv1alpha1.IngressSpec{
			Visibility: networkingv1alpha1.IngressVisibilityExternalIP,
			Rules: []networkingv1alpha1.IngressRule{{
				Hosts: []string{"test.default.svc.cluster.local"},
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
					DomainInternal: "cluster-local-gateway." + serviceMeshNamespace + ".svc.cluster.local",
				}},
			},
		},
	}
)

func TestClusterLocalSvc(t *testing.T) {
	logf.SetLogger(logf.ZapLogger(true))
	ingress := defaultIngressForClusterLocal.DeepCopy()

	// A ServiceMeshMemberRole resource with metadata
	smmr := &maistrav1.ServiceMeshMemberRoll{
		ObjectMeta: metav1.ObjectMeta{
			Name:      smmrName,
			Namespace: serviceMeshNamespace,
		},
	}
	// Register operator types with the runtime scheme.
	s := scheme.Scheme
	s.AddKnownTypes(maistrav1.SchemeGroupVersion, smmr)
	s.AddKnownTypes(networkingv1alpha1.SchemeGroupVersion, ingress)
	s.AddKnownTypes(routev1.SchemeGroupVersion, &routev1.Route{})
	s.AddKnownTypes(routev1.SchemeGroupVersion, &routev1.RouteList{})

	// Create a fake client to mock API calls.
	cl := fake.NewFakeClient(smmr, ingress)

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
	// Check if namespace has been added to smmr
	if err := cl.Get(context.TODO(), types.NamespacedName{Name: smmrName, Namespace: serviceMeshNamespace}, smmr); err != nil {
		t.Fatalf("failed to get ServiceMeshMemberRole: (%v)", err)
	}
	assert.Equal(t, []string{namespace}, smmr.Spec.Members)
}

func TestRouteMigration(t *testing.T) {
	logf.SetLogger(logf.ZapLogger(true))

	test := struct {
		name  string
		state []routev1.Route
		want  []routev1.Route
	}{
		name: "Clean up old route and new route is generated",

		state: []routev1.Route{{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "to-be-reconciled-0",
				Labels: map[string]string{networking.IngressLabelKey: name, serving.RouteLabelKey: name, serving.RouteNamespaceLabelKey: namespace},
			},
			Spec: routev1.RouteSpec{Host: domainName},
		}, {
			ObjectMeta: metav1.ObjectMeta{
				Name:   "no-remove-missing-label",
				Labels: map[string]string{networking.IngressLabelKey: name, serving.RouteLabelKey: name},
			},
			Spec: routev1.RouteSpec{Host: "b.example.com"},
		}, {
			ObjectMeta: metav1.ObjectMeta{
				Name:   "no-remove-other-label",
				Labels: map[string]string{networking.IngressLabelKey: "another", serving.RouteLabelKey: name, serving.RouteNamespaceLabelKey: namespace},
			},
			Spec: routev1.RouteSpec{Host: "c.example.com"},
		}},
		want: []routev1.Route{{
			ObjectMeta: metav1.ObjectMeta{
				Name:            routeName0,
				Namespace:       serviceMeshNamespace,
				Labels:          map[string]string{networking.IngressLabelKey: name, serving.RouteLabelKey: name, serving.RouteNamespaceLabelKey: namespace},
				Annotations:     map[string]string{resources.TimeoutAnnotation: "5s", networking.IngressClassAnnotationKey: network.IstioIngressClassName},
				OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(defaultIngress)},
			},
			Spec: routev1.RouteSpec{
				Host: domainName,
				To: routev1.RouteTargetReference{
					Kind: "Service",
					Name: "istio-ingressgateway",
				},
				Port: &routev1.RoutePort{
					TargetPort: intstr.FromString("http2"),
				},
			},
		}, {
			ObjectMeta: metav1.ObjectMeta{
				Name:   "no-remove-missing-label",
				Labels: map[string]string{networking.IngressLabelKey: name, serving.RouteLabelKey: name},
			},
			Spec: routev1.RouteSpec{Host: "b.example.com"},
		}, {
			ObjectMeta: metav1.ObjectMeta{
				Name:   "no-remove-other-label",
				Labels: map[string]string{networking.IngressLabelKey: "another", serving.RouteLabelKey: name, serving.RouteNamespaceLabelKey: namespace},
			},
			Spec: routev1.RouteSpec{Host: "c.example.com"},
		}},
	}

	t.Run(test.name, func(t *testing.T) {
		ingress := defaultIngress.DeepCopy()

		// A ServiceMeshMemberRole resource with metadata
		smmr := &maistrav1.ServiceMeshMemberRoll{
			ObjectMeta: metav1.ObjectMeta{
				Name:      smmrName,
				Namespace: serviceMeshNamespace,
			},
		}
		// Register operator types with the runtime scheme.
		s := scheme.Scheme
		s.AddKnownTypes(maistrav1.SchemeGroupVersion, smmr)
		s.AddKnownTypes(networkingv1alpha1.SchemeGroupVersion, ingress)
		s.AddKnownTypes(routev1.SchemeGroupVersion, &routev1.Route{})
		s.AddKnownTypes(routev1.SchemeGroupVersion, &routev1.RouteList{})

		// Create a fake client to mock API calls.
		cl := fake.NewFakeClient(smmr, ingress, &routev1.RouteList{Items: test.state})

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
		// Check if namespace has been added to smmr
		if err := cl.Get(context.TODO(), types.NamespacedName{Name: smmrName, Namespace: serviceMeshNamespace}, smmr); err != nil {
			t.Fatalf("failed to get ServiceMeshMemberRole: (%v)", err)
		}
		assert.Equal(t, []string{namespace}, smmr.Spec.Members)

		routeList := &routev1.RouteList{}
		err := cl.List(context.TODO(), &client.ListOptions{}, routeList)
		assert.Nil(t, err)

		routes := routeList.Items
		assert.ElementsMatch(t, routes, test.want)

		// A ServiceMeshMemberRole resource with metadata
		smmrDelete := &maistrav1.ServiceMeshMemberRoll{
			ObjectMeta: metav1.ObjectMeta{
				Name:      smmrName,
				Namespace: serviceMeshNamespace,
			},
		}
		delIngress := deleteIngress.DeepCopy()
		s = scheme.Scheme
		s.AddKnownTypes(maistrav1.SchemeGroupVersion, smmrDelete)
		s.AddKnownTypes(networkingv1alpha1.SchemeGroupVersion, &networkingv1alpha1.IngressList{})
		// Create a fake client to mock API calls.
		clNew := fake.NewFakeClient(smmrDelete, delIngress)

		// Create a Reconcile Ingress object with the scheme and fake client.
		r = &ReconcileIngress{base: &common.BaseIngressReconciler{Client: clNew}, client: clNew, scheme: s}
		// Mock request to simulate Reconcile() being called on an event for a
		// watched resource .
		req = reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      name,
				Namespace: namespace,
			},
		}
		if _, err := r.Reconcile(req); err != nil {
			t.Fatalf("reconcile: (%v)", err)
		}
		if err := clNew.Get(context.TODO(), types.NamespacedName{Name: smmrName, Namespace: serviceMeshNamespace}, smmrDelete); err != nil {
			t.Fatalf("failed to get ServiceMeshMemberRole: (%v)", err)
		}
		// Check if namespace has been removed from smmr
		assert.Equal(t, len([]string{}), len(smmrDelete.Spec.Members))
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
			want:        map[string]string{resources.TimeoutAnnotation: "5s", networking.IngressClassAnnotationKey: network.IstioIngressClassName},
			wantErr:     func(err error) bool { return err == nil },
		},
		{
			name:        "reconcile route with taking over annotations",
			annotations: map[string]string{serving.CreatorAnnotation: "userA", serving.UpdaterAnnotation: "userB"},
			want:        map[string]string{serving.CreatorAnnotation: "userA", serving.UpdaterAnnotation: "userB", resources.TimeoutAnnotation: "5s", networking.IngressClassAnnotationKey: network.IstioIngressClassName},
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
			want:        map[string]string{resources.TLSTerminationAnnotation: "passthrough", resources.TimeoutAnnotation: "5s", networking.IngressClassAnnotationKey: network.IstioIngressClassName},
			wantErr:     func(err error) bool { return err == nil },
		},
		{
			name:        "reconcile route with invalid TLS termination annotation",
			annotations: map[string]string{resources.TLSTerminationAnnotation: "edge"},
			want:        nil,
			wantErr:     errors.IsNotFound,
		},
		{
			name:        "reconcile route with different ingress annotation",
			annotations: map[string]string{networking.IngressClassAnnotationKey: "kourier"},
			want:        map[string]string{networking.IngressClassAnnotationKey: "kourier", resources.TimeoutAnnotation: "5s"},
			wantErr:     func(err error) bool { return err == nil },
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ingress := defaultIngress.DeepCopy()

			// Set test annotations
			annotations := ingress.GetAnnotations()
			for k, v := range test.annotations {
				annotations[k] = v
			}
			ingress.SetAnnotations(annotations)

			// route object
			route := &routev1.Route{}

			// A ServiceMeshMemberRole resource with metadata
			smmr := &maistrav1.ServiceMeshMemberRoll{
				ObjectMeta: metav1.ObjectMeta{
					Name:      smmrName,
					Namespace: serviceMeshNamespace,
				},
			}

			// Register operator types with the runtime scheme.
			s := scheme.Scheme
			s.AddKnownTypes(maistrav1.SchemeGroupVersion, smmr)
			s.AddKnownTypes(networkingv1alpha1.SchemeGroupVersion, ingress)
			s.AddKnownTypes(routev1.SchemeGroupVersion, route)
			s.AddKnownTypes(routev1.SchemeGroupVersion, &routev1.RouteList{})
			s.AddKnownTypes(networkingv1alpha1.SchemeGroupVersion, &networkingv1alpha1.IngressList{})
			// Create a fake client to mock API calls.
			cl := fake.NewFakeClient(smmr, ingress, route)
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

			// Check if namespace has been added to smmr
			if err := cl.Get(context.TODO(), types.NamespacedName{Name: smmrName, Namespace: serviceMeshNamespace}, smmr); err != nil {
				t.Fatalf("failed to get ServiceMeshMemberRole: (%v)", err)
			}
			if ingress.GetAnnotations()[networking.IngressClassAnnotationKey] == network.IstioIngressClassName {
				assert.Equal(t, []string{namespace}, smmr.Spec.Members)
			} else {
				assert.Equal(t, 0, len(smmr.Spec.Members))
			}
			// Check if route has been created
			routes := &routev1.Route{}
			err := cl.Get(context.TODO(), types.NamespacedName{Name: routeName0, Namespace: serviceMeshNamespace}, routes)

			assert.True(t, test.wantErr(err))
			assert.Equal(t, test.want, routes.ObjectMeta.Annotations)
		})
	}
}
