#!/bin/bash
# Copyright 2026 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -e
set -u
set -o pipefail

ROOT=$(git rev-parse --show-toplevel)
cd "${ROOT}"

if [[ -n "${PROJECT_ID:-}" ]]; then
  export KO_DOCKER_REPO="gcr.io/${PROJECT_ID}"
  echo "Using KO_DOCKER_REPO: ${KO_DOCKER_REPO}" >&2
fi

export KO_DEFAULTPLATFORMS="${KO_DEFAULTPLATFORMS:-linux/amd64}"

# ANSI color codes for prettier output
COLOR_CYAN='\033[1;36m'
COLOR_RESET='\033[0m'

function log_step() {
  local step_name="$1"
  echo -e "${COLOR_CYAN}[step]: ${step_name}${COLOR_RESET}"
}

# wait_with_spinner runs a blocking command while showing a simple spinner on an
# interactive terminal, then reports "done"/"failed" and returns the command's
# exit status.
wait_with_spinner() {
  local msg="$1"; shift
  if [[ ! -t 2 ]]; then
    "$@"
    return $?
  fi

  local out; out="$(mktemp)"
  "$@" >"${out}" 2>&1 &
  local pid=$! frames='|/-\' i=0
  while kill -0 "${pid}" 2>/dev/null; do
    i=$(( (i + 1) % ${#frames} ))
    printf '\r%s %s' "${frames:${i}:1}" "${msg}" >&2
    sleep 0.1
  done

  local status=0
  wait "${pid}" || status=$?
  if [[ "${status}" -eq 0 ]]; then
    printf '\r\033[K%s... done\n' "${msg}" >&2
  else
    printf '\r\033[K%s... failed\n' "${msg}" >&2
    cat "${out}" >&2
  fi
  rm -f "${out}"
  return "${status}"
}

function usage() {
  echo "Usage: $0 [options]"
  echo ""
  echo "Options:"
  echo "  --deploy-ate-env                      Build the image and deploy the ate-env service"
  echo "  --delete-ate-env                      Delete the ate-env service and its namespace"
  echo "  -h, --help                            Show this help message"
}

run_kubectl() {
  kubectl \
    ${KUBECTL_CONTEXT:+--context=${KUBECTL_CONTEXT}} \
    "$@"
}

# build_ate_env_image builds and pushes the ate-env image with ko (the service is
# a pure Go binary, so no Dockerfile is required) and echoes its digest-pinned
# reference on stdout. Requires KO_DOCKER_REPO.
build_ate_env_image() {
  if [[ -n "${ATE_ENV_IMAGE:-}" ]]; then
    echo "${ATE_ENV_IMAGE}"
    return
  fi
  if [[ -z "${KO_DOCKER_REPO:-}" ]]; then
    echo "Error: KO_DOCKER_REPO environment variable must be set (or set PROJECT_ID)" >&2
    exit 1
  fi
  if ! command -v ko >/dev/null 2>&1; then
    echo "Error: 'ko' not found in PATH. Install it from https://ko.build." >&2
    exit 1
  fi

  local image_ref
  log_step "build_ate_env_image" >&2
  image_ref="$(ko build --platform=linux/amd64 -B ./cmd/ate-env)"

  if [[ "${image_ref}" != *@sha256:* ]]; then
    echo "Error: ko did not return a digest-pinned image (got '${image_ref}')." >&2
    exit 1
  fi
  echo "${image_ref}"
}

deploy_ate_env() {
  log_step "deploy_ate_env"

  # Build and push the image, capturing its digest-pinned reference.
  local ate_env_image
  ate_env_image=$(build_ate_env_image)
  echo "Using ate-env image: ${ate_env_image}" >&2

  # Render the manifest and apply it.
  if ! sed -e "s|\${ATE_ENV_IMAGE}|${ate_env_image}|g" \
      manifests/ate-env-deployment.yaml \
      | run_kubectl apply -f -; then
    echo >&2
    echo "Error: cluster rejected the manifest. Ensure Agent Substrate is installed" >&2
    echo "and reachable at the ateapi endpoint configured in config.yaml." >&2
    exit 1
  fi

  # Wait for the ate-env deployment to become available.
  log_step "wait for deployment/ate-env to be ready"
  wait_with_spinner "waiting for ate-env (timeout ${ATE_ENV_WAIT_TIMEOUT:-5m})" \
    run_kubectl -n ate-env rollout status deployment/ate-env \
    --timeout="${ATE_ENV_WAIT_TIMEOUT:-5m}"

  echo ""
  echo "Forward the ate-env service by running the following command (optional)"
  echo "kubectl port-forward -n ate-env svc/ate-env 7777:7777"
}

# delete_ate_env removes the ate-env service and its namespace.
delete_ate_env() {
  log_step "delete_ate_env"

  run_kubectl delete --ignore-not-found -f manifests/ate-env-deployment.yaml
}

if [ "$#" -eq 0 ]; then
  usage
  exit 1
fi

# If -h or --help appears anywhere in the command line, print the usage and exit.
for arg in "$@"; do
  case "$arg" in
    -h|--help)
      usage
      exit 0
      ;;
  esac
done

while [[ "$#" -gt 0 ]]; do
  case $1 in
    --deploy-ate-env) deploy_ate_env ;;
    --delete-ate-env) delete_ate_env ;;
    *)
      echo "Error: unknown option: $1" >&2
      echo ""
      usage
      exit 1
      ;;
  esac
  shift
done
