 #!/bin/bash

set -e

KUBECONFIG=/tmp/kube-config-otelcol-e2e-testing

kind create cluster --config kind-config.yaml --kubeconfig ${KUBECONFIG}
kind load docker-image otelcontribcol:latest

# install metrics-server
wget -O metrics-server/metrics-server.yaml https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml
kubectl apply --kustomize metrics-server/
kubectl wait --for=condition=ready pod -l k8s-app=metrics-server -n kube-system --timeout=120s

# setup test hpa and workload
kubectl apply -f hpa/
kubectl wait --for=condition=ready pod -l app=busybox --timeout=120s
kubectl wait --for=condition=ScalingActive hpa -lapp=busybox --timeout=120s

# Run TestE2EHPA
cd ../..
go test --run TestE2EHPA e2e_test.go

# generate expected metrics for hpa
#kubectl apply -f  collector-test/
#kubectl wait --for=condition=ready pod -l'app.kubernetes.io/name=opentelemetry-collector' --timeout=60s
#DUMPFILE="/tmp/dumpcol/dump.json"
#while [ ! -s "${DUMPFILE}" ]; do
#    echo "File is empty, waiting for collection"
#    sleep 5
#done
#kubectl delete -f collector-test/
#yq -P '.' ${DUMPFILE} -o=yaml > expected_hpa_only.yaml


