#!/usr/bin/env bash
# Render the Helm chart with several value sets, then validate the manifests with kubeconform
# (Kubernetes schemas) and kube-linter (security and hygiene). Tool versions are pinned in the
# Makefile and passed in the environment. Needs: helm, go.
set -euo pipefail
cd "$(dirname "$0")/.."

if ! command -v helm >/dev/null; then
  echo "check-helm: helm is not installed, install it (https://helm.sh/docs/intro/install/)" >&2
  exit 1
fi
: "${KUBECONFORM_VERSION:?set by make}"
: "${KUBE_LINTER_VERSION:?set by make}"
: "${KUBE_VERSION:=1.31.0}"

chart=deploy/helm/biblisann
out="$(mktemp -d)"
trap 'rm -rf "$out"' EXIT

render() { # name, helm args...
  local name=$1
  shift
  helm template biblisann "$chart" "$@" >"$out/$name.yaml"
}

render external --set database.external.existingSecret=db
render metrics --set database.external.existingSecret=db --set metrics.tokenSecret.name=metrics
render cnpg --set database.mode=cnpg --set ingress.enabled=true

# CRDs (CloudNativePG, Prometheus Operator) have no schema in the default catalogue.
GOMEMLIMIT=1GiB go run "github.com/yannh/kubeconform/cmd/kubeconform@${KUBECONFORM_VERSION}" \
  -strict -summary -ignore-missing-schemas -kubernetes-version "$KUBE_VERSION" "$out"/*.yaml

GOMEMLIMIT=1GiB go run "golang.stackrox.io/kube-linter/cmd/kube-linter@${KUBE_LINTER_VERSION}" \
  lint --config scripts/kube-linter.yaml "$out"
