#!/bin/sh
set -eu

project=${P1_ACCEPTANCE_PROJECT:-coxpanel-p1-acceptance}
case "$project" in
  coxpanel-p1-acceptance|coxpanel-p1-acceptance-*) ;;
  *) echo "refusing non-acceptance project: $project" >&2; exit 2 ;;
esac
case "$project" in
  *[!a-z0-9_-]*) echo "acceptance project contains unsupported characters: $project" >&2; exit 2 ;;
esac
if [ "${#project}" -gt 45 ]; then
  echo "acceptance project name is too long: $project" >&2
  exit 2
fi
: "${P1_TEST_JWT_SECRET:?P1_TEST_JWT_SECRET must be set for the disposable stack}"
export COXPANEL_JWT_SECRET="$P1_TEST_JWT_SECRET"
build_goproxy=${P1_BUILD_GOPROXY:-https://proxy.golang.org}
case "$build_goproxy" in
  *[!A-Za-z0-9:/.,_+=-]*) echo "P1_BUILD_GOPROXY contains unsupported characters" >&2; exit 2 ;;
esac

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
fixtures_dir=${P1_TEST_FIXTURES_DIR:-$root/deployments/acceptance/fixtures}
case "$fixtures_dir" in
  /*) ;;
  *) echo "P1_TEST_FIXTURES_DIR must be an absolute path" >&2; exit 2 ;;
esac
test -d "$fixtures_dir" || { echo "P1_TEST_FIXTURES_DIR must name an existing test-owned directory" >&2; exit 2; }

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

with_agent=${P1_TEST_WITH_AGENT:-0}
case "$with_agent" in
  0|1) ;;
  *) echo "P1_TEST_WITH_AGENT must be 0 or 1" >&2; exit 2 ;;
esac
if [ "$with_agent" = 1 ]; then
  : "${P1_TEST_AGENT_CREDENTIAL:?P1_TEST_AGENT_CREDENTIAL is required with P1_TEST_WITH_AGENT=1}"
  : "${P1_TEST_AGENT_NODE_ID:?P1_TEST_AGENT_NODE_ID is required with P1_TEST_WITH_AGENT=1}"
  export COXPANEL_AGENT_CREDENTIAL="$P1_TEST_AGENT_CREDENTIAL"
  case "$P1_TEST_AGENT_NODE_ID" in
    ''|*[!0-9]*) echo "P1_TEST_AGENT_NODE_ID must be a numeric test-owned node id" >&2; exit 2 ;;
  esac
  test "$P1_TEST_AGENT_NODE_ID" -gt 0 || { echo "P1_TEST_AGENT_NODE_ID must be greater than zero" >&2; exit 2; }
fi

network="${project}_default"
db_volume="${project}_dbdata"
agent_volume="${project}_agentstate"
db_container="${project}-db-1"
backend_container="${project}-backend-1"
frontend_container="${project}-frontend-1"
target_container="${project}-target-1"
agent_container="${project}-agent-1"
backend_image="${project}/backend:review"
frontend_image="${project}/frontend:review"
agent_image="${project}/agent:review"

if docker network inspect "$network" >/dev/null 2>&1; then
  echo "refusing to reuse existing acceptance network: $network" >&2
  exit 1
fi
if docker volume inspect "$db_volume" >/dev/null 2>&1; then
  echo "refusing to reuse existing acceptance volume: $db_volume" >&2
  exit 1
fi
for container in "$db_container" "$backend_container" "$frontend_container" "$target_container" "$agent_container"; do
  if docker inspect "$container" >/dev/null 2>&1; then
    echo "refusing to reuse existing acceptance container: $container" >&2
    exit 1
  fi
done
if [ "$with_agent" = 1 ] && docker volume inspect "$agent_volume" >/dev/null 2>&1; then
  echo "refusing to reuse existing acceptance volume: $agent_volume" >&2
  exit 1
fi

label_args() {
  printf '%s\n' \
    --label "com.docker.compose.project=$project" \
    --label "com.docker.compose.service=$1"
}

docker network create --internal \
  --label "com.docker.compose.project=$project" \
  --label com.docker.compose.network=default \
  "$network" >/dev/null
docker volume create \
  --label "com.docker.compose.project=$project" \
  --label com.docker.compose.volume=dbdata \
  "$db_volume" >/dev/null

docker build --platform linux/amd64 --build-arg "GOPROXY=$build_goproxy" -f backend/Dockerfile -t "$backend_image" "$root"
docker build --platform linux/amd64 -f frontend/Dockerfile -t "$frontend_image" "$root"
docker build --platform linux/amd64 -f agent/Dockerfile -t "$agent_image" "$root"

set -- $(label_args db)
docker run -d \
  --name "$db_container" \
  "$@" \
  --network "$network" \
  --network-alias db \
  --user 70:70 \
  --read-only \
  --tmpfs /run/postgresql:uid=70,gid=70,mode=0755 \
  --tmpfs /tmp:uid=70,gid=70,mode=1777 \
  --security-opt no-new-privileges:true \
  --cap-drop ALL \
  --stop-timeout 30 \
  --mount "type=volume,src=$db_volume,dst=/var/lib/postgresql/data" \
  --env POSTGRES_DB=p1test \
  --env POSTGRES_USER=p1test \
  --env POSTGRES_HOST_AUTH_METHOD=trust \
  --health-cmd='pg_isready -U p1test -d p1test' \
  --health-interval=3s \
  --health-timeout=3s \
  --health-retries=20 \
  postgres:16-alpine postgres -c 'listen_addresses=*'

attempt=0
while :; do
  health=$(docker inspect --format '{{.State.Health.Status}}' "$db_container" 2>/dev/null || true)
  case "$health" in
    healthy) break ;;
    unhealthy) echo "acceptance database became unhealthy" >&2; exit 1 ;;
  esac
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 60 ]; then
    echo "acceptance database did not become healthy" >&2
    exit 1
  fi
  sleep 1
done

set -- $(label_args backend)
docker run -d \
  --name "$backend_container" \
  "$@" \
  --network "$network" \
  --network-alias backend \
  --read-only \
  --tmpfs /tmp \
  --security-opt no-new-privileges:true \
  --cap-drop ALL \
  --stop-timeout 15 \
  --env COXPANEL_DB_URL=postgres://p1test@db:5432/p1test?sslmode=disable \
  --env COXPANEL_JWT_SECRET \
  --env COXPANEL_ADDR=:8080 \
  "$backend_image"

set -- $(label_args frontend)
docker run -d \
  --name "$frontend_container" \
  "$@" \
  --network "$network" \
  --network-alias frontend \
  --publish "127.0.0.1:${frontend_port}:8080" \
  --read-only \
  --tmpfs /tmp:mode=1777 \
  --tmpfs /var/cache/nginx:mode=1777 \
  --tmpfs /var/run:mode=1777 \
  --security-opt no-new-privileges:true \
  --cap-drop ALL \
  --stop-timeout 15 \
  "$frontend_image"

set -- $(label_args target)
docker run -d \
  --name "$target_container" \
  "$@" \
  --network "$network" \
  --network-alias target \
  --publish "127.0.0.1:${target_port}:8080" \
  --read-only \
  --tmpfs /tmp:mode=1777 \
  --tmpfs /var/cache/nginx:mode=1777 \
  --tmpfs /var/run:mode=1777 \
  --security-opt no-new-privileges:true \
  --cap-drop ALL \
  --stop-timeout 15 \
  --mount "type=bind,src=$root/deployments/acceptance/target,dst=/usr/share/nginx/html,readonly" \
  nginxinc/nginx-unprivileged:1.27-alpine

if [ "$with_agent" = 1 ]; then
  docker volume create \
    --label "com.docker.compose.project=$project" \
    --label com.docker.compose.volume=agentstate \
    "$agent_volume" >/dev/null
  set -- $(label_args agent)
  docker run -d \
    --name "$agent_container" \
    "$@" \
    --platform linux/amd64 \
    --network "$network" \
    --network-alias agent \
    --publish "127.0.0.1:${reality_port}:18443" \
    --publish "127.0.0.1:${ss2022_port}:18388" \
    --publish "127.0.0.1:${hysteria2_port}:18444/udp" \
    --read-only \
    --tmpfs /tmp:mode=1777 \
    --security-opt no-new-privileges:true \
    --cap-drop ALL \
    --stop-timeout 15 \
    --mount "type=volume,src=$agent_volume,dst=/var/lib/coxpanel" \
    --mount "type=bind,src=$fixtures_dir,dst=/var/lib/coxpanel/fixtures,readonly" \
    --env COXPANEL_AGENT_CREDENTIAL \
    "$agent_image" \
    -panel http://backend:8080 \
    -node "$P1_TEST_AGENT_NODE_ID" \
    -config /var/lib/coxpanel/config.json \
    -interval 5s
fi

echo "disposable acceptance stack is running as $project"
echo "frontend: http://127.0.0.1:$frontend_port"
echo "target:   http://127.0.0.1:$target_port"
echo "cleanup:  P1_ACCEPTANCE_PROJECT=$project deployments/acceptance/down.sh"
