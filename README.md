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

# Installation

## Create an OpenShift 4 cluster

Follow the instructions at https://try.openshift.com/ to create an
OpenShift 4 cluster. Everything below assumes your cluster is up and
the `oc` command can successfully authenticate with your cluster.

## Install Knative

Follow the instructions and script at
https://github.com/openshift-cloud-functions/knative-operators to
deploy Knative to your OpenShift 4 cluster. Using that script will
install Knative via Operators and ensure default values get customized
for your specific OpenShift 4 environment.

# Deploy the OpenShift Ingress Operator

```shell
oc apply --filename https://github.com/bbrowning/knative-openshift-ingress/releases/download/v0.0.1/release.yaml
```

## Deploy the Knative helloworld-go sample

```shell
cat <<EOF | kubectl apply -f -
apiVersion: serving.knative.dev/v1alpha1
kind: Service
metadata:
  name: helloworld-go
  namespace: default
spec:
  runLatest:
    configuration:
      revisionTemplate:
        spec:
          container:
            image: gcr.io/knative-samples/helloworld-go
            env:
              - name: TARGET
                value: "Go Sample v1"
EOF
```

Wait for the helloworld-go Knative Service to become Ready:
```shell
kubectl get ksvc helloworld-go -n default
```

Copy and paste the `DOMAIN` value for your Knative Service into a web
browser and confirm that it works.

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
