#!/usr/bin/env bash
#
# Point the chart's artifacthub.io/images annotation at the image its default
# values install: the repository from values.yaml, tagged "v" plus Chart.yaml's
# appVersion.
#
# Usage: sync-chart-images.sh [--check]
#
# --check reports and exits non-zero instead of writing, for CI.

set -euo pipefail

REPO_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
CHART_DIR="${REPO_ROOT}/deploy/helm/credential-provider-harbor"
CHART="${CHART_DIR}/Chart.yaml"
VALUES="${CHART_DIR}/values.yaml"

check_only=false
[ "${1:-}" != "--check" ] || check_only=true

# Read with awk rather than a YAML tool, which is what the Taskfile already
# does for appVersion, and means this runs wherever bash does.
app_version=$(awk '/^appVersion:/ {gsub(/"/, "", $2); print $2; exit}' "${CHART}")
repository=$(awk '/^image:/ {f=1} f && /^  repository:/ {print $2; exit}' "${VALUES}")
[ -n "${app_version}" ] || { echo "ERROR: no appVersion in ${CHART}" >&2; exit 1; }
[ -n "${repository}" ] || { echo "ERROR: no image.repository in ${VALUES}" >&2; exit 1; }

want="${repository}:v${app_version}"
current_image() {
  awk '/^  artifacthub.io\/images:/ {f=1; next} f && /^  [^ ]/ {exit} f && $1 == "image:" {print $2; exit}' "${CHART}"
}
have=$(current_image)

if [ "${have}" = "${want}" ]; then
  echo "chart images annotation already names ${want}"
  exit 0
fi

if [ "${check_only}" = true ]; then
  echo "ERROR: the chart's images annotation does not name the image it installs." >&2
  echo "  annotation: ${have:-<missing>}" >&2
  echo "  installs:   ${want}" >&2
  echo "  Run scripts/sync-chart-images.sh." >&2
  exit 1
fi

[ -n "${have}" ] || { echo "ERROR: no artifacthub.io/images entry in ${CHART} to update" >&2; exit 1; }

tmp=$(mktemp)
# Anchored on the value it already has, so nothing else in the file can match.
sed "s|^\([[:space:]]*image: \)${have}\$|\1${want}|" "${CHART}" > "${tmp}"
mv "${tmp}" "${CHART}"

have=$(current_image)
[ "${have}" = "${want}" ] || {
  echo "ERROR: rewrote ${CHART} but the annotation still reads ${have}" >&2
  exit 1
}
echo "chart images annotation now names ${want}"
