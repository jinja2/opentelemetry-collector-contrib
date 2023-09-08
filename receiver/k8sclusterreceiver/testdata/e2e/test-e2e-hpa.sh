 #!/bin/bash

set -e

GENERATE_EXAMPLE_METRICS=false
KUBECONFIG=/tmp/kube-config-otelcol-e2e-testing

setup_cluster() {
  kind create cluster --config kind-config.yaml --kubeconfig ${KUBECONFIG}
  kind load docker-image otelcontribcol:latest
}

setup_metrics_server() {
  mkdir tmp-metrics-server
  wget -O tmp-metrics-server/metrics-server.yaml https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml
  cat > tmp-metrics-server/kustomization.yaml <<EOF
resources:
  - metrics-server.yaml
patches:
  - target:
      group: apps
      version: v1
      kind: Deployment
      name: metrics-server
      namespace: kube-system
    patch: |
      - op: add
        path: /spec/template/spec/containers/0/args/-
        value: --kubelet-insecure-tls
EOF
  kubectl apply --kustomize tmp-metrics-server/
  kubectl wait --for=condition=ready pod -l k8s-app=metrics-server -n kube-system --timeout=120s
  rm -rf tmp-metrics-server
}

setup_test_hpa() {
  kubectl apply -f hpa/
  kubectl wait --for=condition=ready pod -l app=busybox --timeout=120s
  kubectl wait --for=condition=ScalingActive hpa -lapp=busybox --timeout=120s
}

get_example_metrics() {
  ## generate expected metrics for hpa
  DUMPFILE="/tmp/dumpcol/dump.json"
  shopt -s nullglob
  for file in ./collector/*; do
    if [[ $file =~ configmap|deployment ]]; then
      continue
    fi
    sed 's/{{ .Name }}/otel-test/g' $file | kubectl apply -f -
  done
  kubectl apply -f hpa_collector.yaml
  kubectl wait --for=condition=ready pod -l'app.kubernetes.io/name=opentelemetry-collector' --timeout=60s

  while [ ! -s "${DUMPFILE}" ]; do
      echo "File is empty, waiting for collection"
      sleep 5
  done
  for file in ./collector/*; do
    sed 's/{{ .Name }}/otel-test/g' $file | kubectl delete -f -
  done
  yq -P '.' ${DUMPFILE} -o=yaml > expected_hpa_only.yaml
}

setup_cluster
setup_metrics_server
setup_test_hpa
if [ "$GENERATE_EXAMPLE_METRICS" == "true" ]; then
  get_example_metrics
fi

cd ../..
go test --run TestE2EHPA e2e_test.go -v

kind delete cluster --name kind