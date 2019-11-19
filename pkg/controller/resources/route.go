package resources

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"

	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"knative.dev/pkg/kmeta"
	"knative.dev/serving/pkg/apis/networking"
	networkingv1alpha1 "knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	TimeoutAnnotation      = "haproxy.router.openshift.io/timeout"
	DisableRouteAnnotation = "serving.knative.openshift.io/disableRoute"
	CertificateAnnotation  = "serving.knative.openshift.io/certificate"
)

// ErrNoValidLoadbalancerDomain indicates that the current ingress does not have a DomainInternal field, or
// said field does not contain a value we can work with.
var ErrNoValidLoadbalancerDomain = errors.New("unable to find ClusterIngress LoadBalancer with DomainInternal set")

// MakeRoutes creates OpenShift Routes from a Knative Ingress
func MakeRoutes(ci networkingv1alpha1.IngressAccessor, client client.Client) ([]*routev1.Route, error) {
	routes := []*routev1.Route{}

	// Skip all route creation for cluster-local ingresses.
	if ci.GetSpec().Visibility == networkingv1alpha1.IngressVisibilityClusterLocal {
		return routes, nil
	}

	for _, rule := range ci.GetSpec().Rules {
		for _, host := range rule.Hosts {
			// Ignore domains like myksvc.myproject.svc.cluster.local
			// TODO: This also ignores any top-level vanity domains
			// like foo.com the user may have set. But, it tackles the
			// autogenerated name case which is the biggest pain
			// point.
			parts := strings.Split(host, ".")
			if len(parts) > 2 && parts[2] != "svc" {
				route, err := makeRoute(ci, client, host, rule)
				if err != nil {
					return nil, err
				}
				if route == nil {
					continue
				}
				routes = append(routes, route)
			}
		}
	}

	return routes, nil
}

func makeRoute(ci networkingv1alpha1.IngressAccessor, client client.Client, host string, rule networkingv1alpha1.IngressRule) (*routev1.Route, error) {
	// Take over annotaitons from ingress.
	annotations := ci.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	// Skip making route when visibility of the rule is local only.
	if rule.Visibility == networkingv1alpha1.IngressVisibilityClusterLocal {
		return nil, nil
	}

	// Skip making route when the annotation is specified.
	if _, ok := annotations[DisableRouteAnnotation]; ok {
		return nil, nil
	}

	if rule.HTTP != nil {
		for i := range rule.HTTP.Paths {
			if rule.HTTP.Paths[i].Timeout != nil {
				// Supported time units for openshift route annotations are microseconds (us), milliseconds (ms), seconds (s), minutes (m), hours (h), or days (d)
				// But the timeout value from ingress is in xmys(ex: 10m0s) format
				// So, in order to make openshift route to work converting it into seconds.
				annotations[TimeoutAnnotation] = fmt.Sprintf("%vs", rule.HTTP.Paths[i].Timeout.Duration.Seconds())
			} else {
				/* Currently v0.5.0 of serving code does not have "DefaultMaxRevisionTimeoutSeconds" So hard coding "timeout" value.
				Once serving updated to latest version then will remove hard coded value and update with
				annotations[TimeoutAnnotation] = fmt.Sprintf("%vs", config.DefaultMaxRevisionTimeoutSeconds) */
				annotations[TimeoutAnnotation] = "600s"
			}

		}
	}

	labels := make(map[string]string)
	labels[networking.IngressLabelKey] = ci.GetName()

	ingressLabels := ci.GetLabels()
	labels[serving.RouteLabelKey] = ingressLabels[serving.RouteLabelKey]
	labels[serving.RouteNamespaceLabelKey] = ingressLabels[serving.RouteNamespaceLabelKey]

	name := routeName(string(ci.GetUID()), host)
	serviceName := ""
	namespace := ""
	if ci.GetStatus().LoadBalancer != nil {
		for _, lbIngress := range ci.GetStatus().LoadBalancer.Ingress {
			if lbIngress.DomainInternal != "" {
				// DomainInternal should look something like:
				// istio-ingressgateway.istio-system.svc.cluster.local
				parts := strings.Split(lbIngress.DomainInternal, ".")
				if len(parts) > 2 && parts[2] == "svc" {
					serviceName = parts[0]
					namespace = parts[1]
				}
			}
		}
	}

	if serviceName == "" || namespace == "" {
		return nil, ErrNoValidLoadbalancerDomain
	}

	secret := &corev1.Secret{}
	if cert, ok := annotations[CertificateAnnotation]; ok {
		err := client.Get(context.TODO(), types.NamespacedName{Namespace: ci.GetNamespace(), Name: cert}, secret)
		if err != nil {
			return nil, err
		}
	}

	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       namespace,
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(ci)},
			Labels:          labels,
			Annotations:     annotations,
		},
		Spec: routev1.RouteSpec{
			Host: host,
			Port: &routev1.RoutePort{
				TargetPort: intstr.FromString("http2"),
			},
			To: routev1.RouteTargetReference{
				Kind: "Service",
				Name: serviceName,
			},
			TLS: &routev1.TLSConfig{
				Certificate:                   string(secret.Data["tls.crt"]),
				Key:                           string(secret.Data["tls.key"]),
				CACertificate:                 string(secret.Data["caCertificate"]),
				Termination:                   routev1.TLSTerminationEdge,
				InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyAllow,
			},
		},
	}

	return route, nil
}

func routeName(uid, host string) string {
	return fmt.Sprintf("route-%s-%x", uid, hashHost(host))
}

func hashHost(host string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(host)))[0:6]
}
