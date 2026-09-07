#!/bin/sh
set -eu

project=${P1_ACCEPTANCE_PROJECT:-coxpanel-p1-acceptance}
case "$project" in
  coxpanel-p1-acceptance|coxpanel-p1-acceptance-*) ;;
  *) echo "refusing non-acceptance compose project: $project" >&2; exit 2 ;;
esac
case "$project" in
  *[!a-z0-9_-]*) echo "acceptance project contains unsupported characters: $project" >&2; exit 2 ;;
esac
if [ "${#project}" -gt 45 ]; then
  echo "acceptance project name is too long: $project" >&2
  exit 2
fi
: "${P1_TEST_JWT_SECRET:?P1_TEST_JWT_SECRET must be set for the disposable stack}"

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
fixtures_dir=${P1_TEST_FIXTURES_DIR:-$root/deployments/acceptance/fixtures}
case "$fixtures_dir" in
  /*) ;;
  *) echo "P1_TEST_FIXTURES_DIR must be an absolute path" >&2; exit 2 ;;
esac
test -d "$fixtures_dir" || { echo "P1_TEST_FIXTURES_DIR must name an existing test-owned directory" >&2; exit 2; }
export P1_TEST_FIXTURES_DIR="$fixtures_dir"

valid_port() {
  port=$1
  case "$port" in
    ''|*[!0-9]*|??????*) return 1 ;;
  esac
  test "$port" -ge 1 2>/dev/null && test "$port" -le 65535 2>/dev/null
}

frontend_port=${P1_TEST_FRONTEND_PORT:-18173}
target_port=${P1_TEST_TARGET_PORT:-18180}
reality_port=${P1_TEST_REALITY_PORT:-18443}
ss2022_port=${P1_TEST_SS2022_PORT:-18388}
hysteria2_port=${P1_TEST_HYSTERIA2_PORT:-18444}
for port in "$frontend_port" "$target_port" "$reality_port" "$ss2022_port" "$hysteria2_port"; do
  valid_port "$port" || { echo "test published ports must be integers from 1 to 65535" >&2; exit 2; }
done

case "${P1_TEST_WITH_AGENT:-0}" in
  0|1) ;;
  *) echo "P1_TEST_WITH_AGENT must be 0 or 1" >&2; exit 2 ;;
esac

compose_file="$root/deployments/acceptance/docker-compose.yml"
compose() {
  docker compose -p "$project" -f "$compose_file" "$@"
}

if [ "${P1_TEST_WITH_AGENT:-0}" = 1 ]; then
  : "${P1_TEST_AGENT_CREDENTIAL:?P1_TEST_AGENT_CREDENTIAL is required with P1_TEST_WITH_AGENT=1}"
  : "${P1_TEST_AGENT_NODE_ID:?P1_TEST_AGENT_NODE_ID is required with P1_TEST_WITH_AGENT=1}"
  case "$P1_TEST_AGENT_NODE_ID" in
    ''|*[!0-9]*) echo "P1_TEST_AGENT_NODE_ID must be a numeric test-owned node id" >&2; exit 2 ;;
  esac
  test "$P1_TEST_AGENT_NODE_ID" -gt 0 || { echo "P1_TEST_AGENT_NODE_ID must be greater than zero" >&2; exit 2; }
  compose --profile agent build
  compose --profile agent up -d
else
  compose build backend frontend
  compose up -d db backend frontend target
fi

compose ps
echo "disposable acceptance stack is running as $project"
echo "frontend: http://127.0.0.1:$frontend_port"
echo "target:   http://127.0.0.1:$target_port"
echo "cleanup:  deployments/acceptance/down.sh"
