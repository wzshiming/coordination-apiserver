#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_DIR="$(dirname "${BASH_SOURCE[0]}")"
ROOT_DIR="${SCRIPT_DIR}/.."

PACKAGE=github.com/wzshiming/coordination-apiserver

go run k8s.io/kube-openapi/cmd/openapi-gen@v0.0.0-20250610211856-8b98d1ed966a \
    --output-pkg "${PACKAGE}/pkg/openapi" \
    --output-dir "${ROOT_DIR}/pkg/openapi" \
    --output-file zz_generated.openapi.go \
    --go-header-file hack/boilerplate.go.txt \
    "${ROOT_DIR}/vendor/k8s.io/api/coordination/v1" \
    "${ROOT_DIR}/vendor/k8s.io/apimachinery/pkg/apis/meta/v1" \
    "${ROOT_DIR}/vendor/k8s.io/apimachinery/pkg/version" \
    "${ROOT_DIR}/vendor/k8s.io/apimachinery/pkg/runtime"
