#!/usr/bin/env bash
#
# Set up the Harbor side of a federated robot pull: a Trusted Issuer pointed at
# a cluster's OIDC discovery document, a robot account bound to it, and the
# claim rules that decide which service account token is allowed to become that
# robot.
#
# This is the half that cannot be tested in a local cluster, so it is scripted
# for the cloud runs that use a real Harbor. Nothing here touches Kubernetes.
#
# Usage:
#   scripts/harbor-federated-robot.sh setup
#   scripts/harbor-federated-robot.sh status
#   scripts/harbor-federated-robot.sh teardown
#
# Environment, all required unless a default is shown:
#   HARBOR_URL        https://harbor.example.com
#   HARBOR_USER       a user that can administer the project
#   HARBOR_PASS       its password, read from the environment and never logged
#   HARBOR_PROJECT    the project the robot can pull from
#   OIDC_ISSUER       the cluster's issuer URL, for the iss claim rule
#   OIDC_DISCOVERY    default "${OIDC_ISSUER}/.well-known/openid-configuration"
#   OIDC_AUDIENCE     default the HARBOR_URL host
#   SUBJECT           system:serviceaccount:<namespace>:<name>
#   IDP_NAME          default "${HARBOR_PROJECT}-issuer"
#   ROBOT_NAME        default "${HARBOR_PROJECT}-puller"
#   MATCH_AUDIENCE    default 1; set to 0 to skip the aud claim rule
#
# Needs: curl, jq.

set -euo pipefail

need() { command -v "$1" >/dev/null 2>&1 || { echo "need $1" >&2; exit 1; }; }
need curl
need jq

red() { printf '\033[31m%s\033[0m\n' "$1"; }
green() { printf '\033[32m%s\033[0m\n' "$1"; }

require_env() {
  local missing=""
  for v in "$@"; do
    [ -n "${!v:-}" ] || missing="${missing} ${v}"
  done
  [ -z "${missing}" ] || { red "unset:${missing}"; exit 1; }
}

require_env HARBOR_URL HARBOR_USER HARBOR_PASS HARBOR_PROJECT

HARBOR_HOST="${HARBOR_URL#*://}"
HARBOR_HOST="${HARBOR_HOST%%/*}"
API="${HARBOR_URL%/}/api/v2.0"
OIDC_AUDIENCE="${OIDC_AUDIENCE:-${HARBOR_HOST}}"
IDP_NAME="${IDP_NAME:-${HARBOR_PROJECT}-issuer}"
ROBOT_NAME="${ROBOT_NAME:-${HARBOR_PROJECT}-puller}"
MATCH_AUDIENCE="${MATCH_AUDIENCE:-1}"

# Credentials go to curl over stdin rather than on the command line, so they do
# not show up in the process list of a shared machine or in a shell history.
api() {
  local method=$1 path=$2 body=${3:-}
  local args=(-sS -X "${method}" -H 'Accept: application/json')
  if [ -n "${body}" ]; then
    args+=(-H 'Content-Type: application/json' --data-binary "${body}")
  fi
  printf 'user = "%s:%s"\n' "${HARBOR_USER}" "${HARBOR_PASS}" \
    | curl "${args[@]}" -K - "${API}${path}"
}

api_code() {
  local method=$1 path=$2 body=${3:-}
  local args=(-sS -o /dev/null -w '%{http_code}' -X "${method}")
  if [ -n "${body}" ]; then
    args+=(-H 'Content-Type: application/json' --data-binary "${body}")
  fi
  printf 'user = "%s:%s"\n' "${HARBOR_USER}" "${HARBOR_PASS}" \
    | curl "${args[@]}" -K - "${API}${path}"
}

project_id() {
  api GET "/projects?name=${HARBOR_PROJECT}" \
    | jq -r --arg n "${HARBOR_PROJECT}" '.[] | select(.name == $n) | .project_id' \
    | head -1
}

idp_id() {
  api GET "/federated-idps" \
    | jq -r --arg n "${IDP_NAME}" '.[]? | select(.name == $n) | .id' | head -1
}

# Harbor names a project robot "robot$<project>+<name>", so match on the suffix
# rather than on what was asked for.
robot_id() {
  api GET "/robots?page_size=100" \
    | jq -r --arg n "+${ROBOT_NAME}" '.[]? | select(.name | endswith($n)) | .id' | head -1
}

cmd_setup() {
  require_env OIDC_ISSUER SUBJECT
  local discovery="${OIDC_DISCOVERY:-${OIDC_ISSUER%/}/.well-known/openid-configuration}"

  local pid
  pid=$(project_id)
  [ -n "${pid}" ] || { red "project ${HARBOR_PROJECT} not found"; exit 1; }
  echo "project:   ${HARBOR_PROJECT} (id ${pid})"

  # Harbor fetches the discovery document itself, from wherever Harbor runs.
  # Reaching it from here proves nothing, so ask Harbor to do it first: a
  # firewall between Harbor and the cluster issuer fails here, with a clear
  # message, rather than later as a token that will not validate.
  echo "checking Harbor can reach ${discovery}"
  local ping
  ping=$(api POST "/federated-idps/openid-config" \
    "$(jq -nc --arg u "${discovery}" '{openid_config_url: $u}')")
  local derived
  derived=$(echo "${ping}" | jq -r '.issuer // empty')
  if [ -z "${derived}" ]; then
    red "Harbor could not read the discovery document"
    echo "${ping}" >&2
    exit 1
  fi
  if [ "${derived}" != "${OIDC_ISSUER%/}" ]; then
    red "discovery document says issuer ${derived}, expected ${OIDC_ISSUER%/}"
    exit 1
  fi
  green "reachable, issuer ${derived}"

  local iid
  iid=$(idp_id)
  if [ -n "${iid}" ]; then
    echo "issuer:    ${IDP_NAME} (id ${iid}, already there)"
  else
    api POST "/federated-idps" "$(jq -nc \
      --arg name "${IDP_NAME}" \
      --arg url "${discovery}" \
      --argjson pid "${pid}" \
      '{name: $name, openid_config_url: $url, offline_validation: false, project_id: $pid}')" \
      >/dev/null
    iid=$(idp_id)
    [ -n "${iid}" ] || { red "creating the trusted issuer failed"; exit 1; }
    green "issuer:    ${IDP_NAME} (id ${iid}, created)"
  fi

  local rid
  rid=$(robot_id)
  if [ -n "${rid}" ]; then
    echo "robot:     ${ROBOT_NAME} (id ${rid}, already there)"
  else
    # A federated robot has no usable secret: it is the token that authenticates
    # it. The secret the create call returns is therefore deliberately dropped.
    api POST "/robots" "$(jq -nc \
      --arg name "${ROBOT_NAME}" \
      --arg ns "${HARBOR_PROJECT}" \
      --argjson iid "${iid}" \
      '{name: $name, level: "project", duration: -1, federatedidp_id: $iid,
        permissions: [{kind: "project", namespace: $ns,
                       access: [{resource: "repository", action: "pull"},
                                {resource: "repository", action: "list"}]}]}')" \
      >/dev/null
    rid=$(robot_id)
    [ -n "${rid}" ] || { red "creating the robot failed"; exit 1; }
    green "robot:     ${ROBOT_NAME} (id ${rid}, created)"
  fi

  # The claim rules are the authorization decision. Without a sub rule any token
  # this issuer signs would become the robot, which is every service account in
  # the cluster.
  local rules
  rules=$(jq -nc \
    --arg iss "${OIDC_ISSUER%/}" \
    --arg aud "${OIDC_AUDIENCE}" \
    --arg sub "${SUBJECT}" \
    --argjson rid "${rid}" \
    --argjson iid "${iid}" \
    --argjson matchaud "${MATCH_AUDIENCE}" \
    '{rules: ([{claim_path: "iss", value: $iss, robot_id: $rid, identity_provider_id: $iid},
               {claim_path: "sub", value: $sub, robot_id: $rid, identity_provider_id: $iid}]
              + (if $matchaud == 1
                 then [{claim_path: "aud", value: $aud, robot_id: $rid, identity_provider_id: $iid}]
                 else [] end))}')

  local code
  code=$(api_code POST "/federated-idps/${iid}/claims" "${rules}")
  case "${code}" in
    2*) green "claims:    iss, sub$([ "${MATCH_AUDIENCE}" = 1 ] && echo ", aud") bound to robot ${rid}" ;;
    409) echo "claims:    already there (HTTP 409)" ;;
    *)  red "adding claim rules failed (HTTP ${code})"
        api GET "/federated-idps/${iid}/claims" >&2 || true
        exit 1 ;;
  esac

  echo
  echo "a pod using ${SUBJECT} can now pull from ${HARBOR_HOST}/${HARBOR_PROJECT}"
  echo "with no imagePullSecret, as long as the provider asks for audience ${OIDC_AUDIENCE}"
}

cmd_status() {
  echo "=== trusted issuers ==="
  api GET "/federated-idps" | jq '[.[]? | {id, name, issuer, project_id}]'
  local iid
  iid=$(idp_id)
  if [ -n "${iid}" ]; then
    echo "=== claim rules on ${IDP_NAME} ==="
    api GET "/federated-idps/${iid}/claims" | jq '[.[]? | {claim_path, value, robot_id}]'
  fi
  echo "=== robots ==="
  api GET "/robots?page_size=100" | jq '[.[]? | {id, name, level, federatedidp_id, disable}]'
}

# Order matters: the robot references the issuer, so it goes first.
cmd_teardown() {
  local rid iid
  rid=$(robot_id)
  if [ -n "${rid}" ]; then
    echo "deleting robot ${rid} ($(api_code DELETE "/robots/${rid}"))"
  else
    echo "no robot named ${ROBOT_NAME}"
  fi
  iid=$(idp_id)
  if [ -n "${iid}" ]; then
    echo "deleting trusted issuer ${iid} ($(api_code DELETE "/federated-idps/${iid}"))"
  else
    echo "no trusted issuer named ${IDP_NAME}"
  fi
}

case "${1:-}" in
  setup)    cmd_setup ;;
  status)   cmd_status ;;
  teardown) cmd_teardown ;;
  *) sed -n '2,/^set -euo/p' "$0" | sed 's/^# \{0,1\}//;$d' >&2; exit 1 ;;
esac
