# Knative OpenShift Ingress

This Operator ensures an OpenShift Route exists for every Knative
Route (and, by extension, Knative Service) that's visibility allows it
to be exposed publicly.

This allows Knative traffic to get into the cluster via OpenShift's
routing layer instead of having to come in directly from Istio or some
other gateway's LoadBalancer IP.

You still need a specific Knative ClusterIngress implementation to
handle creating the actual traffic splitting. The default Knative
install uses Istio's ingressgateway for this out of the box, and that
is tested to work with this.

# Deploying to OpenShift
```shell
kubectl apply --filename https://github.com/bbrowning/knative-openshift-ingress/releases/download/v0.0.1/release.yaml
```

# Testing changes locally

If you want to hack on this OpenShift ingress implementation, clone
the repo and run the controller locally:

```shell
WATCH_NAMESPACE="" go run cmd/manager/main.go
```

# Building, pushing, and testing changes

This is how I do it, at least. You'll need to change the repos to ones
that aren't bbrowning.

```shell
operator-sdk build quay.io/bbrowning/knative-openshift-ingress:v0.0.1
docker push quay.io/bbrowning/knative-openshift-ingress:v0.0.1
```

Update the image in deploy/release.yaml and tag the git repo with the
same version as the image.
