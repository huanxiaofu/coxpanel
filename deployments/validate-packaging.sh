#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"

for file in backend/Dockerfile frontend/Dockerfile agent/Dockerfile .dockerignore docker-compose.yml deployments/nginx/default.conf deployments/acceptance/docker-compose.yml deployments/acceptance/up.sh deployments/acceptance/down.sh deployments/acceptance/run-no-compose.sh deployments/sing-box-1.13.21.provenance; do
  test -f "$file" || { echo "missing packaging file: $file" >&2; exit 1; }
done

grep -F 'COPY shared ./shared' backend/Dockerfile >/dev/null
grep -F 'COPY shared ./shared' agent/Dockerfile >/dev/null
grep -F 'coxpanel-bootstrap-owner' backend/Dockerfile >/dev/null
grep -F 'ENTRYPOINT ["/usr/local/bin/coxpanel-server"]' backend/Dockerfile >/dev/null
grep -F 'context: .' docker-compose.yml >/dev/null
grep -F 'context: ../..' deployments/acceptance/docker-compose.yml >/dev/null
grep -F 'ARG SING_BOX_VERSION=1.13.21' agent/Dockerfile >/dev/null
grep -F 'sha256sum -c' agent/Dockerfile >/dev/null
grep -F 'sing-box version ${SING_BOX_VERSION}' agent/Dockerfile >/dev/null
grep -F 'provenance_artifact' agent/Dockerfile >/dev/null
grep -F 'provenance_download' agent/Dockerfile >/dev/null
grep -F 'provenance_release_tag' agent/Dockerfile >/dev/null
grep -F 'provenance_immutable' agent/Dockerfile >/dev/null
grep -F 'provenance_asset_id' agent/Dockerfile >/dev/null
grep -F 'COPY deployments/sing-box-1.13.21.provenance' agent/Dockerfile >/dev/null
grep -F 'provenance_sha256' agent/Dockerfile >/dev/null
grep -F 'provenance_size' agent/Dockerfile >/dev/null
grep -F 'artifact=sing-box-1.13.21-linux-amd64-musl.tar.gz' deployments/sing-box-1.13.21.provenance >/dev/null
grep -F 'asset_id=536461728' deployments/sing-box-1.13.21.provenance >/dev/null
grep -F 'asset_size=24759892' deployments/sing-box-1.13.21.provenance >/dev/null
grep -F 'release_immutable=true' deployments/sing-box-1.13.21.provenance >/dev/null
grep -F 'download=https://github.com/SagerNet/sing-box/releases/download/v1.13.21/sing-box-1.13.21-linux-amd64-musl.tar.gz' deployments/sing-box-1.13.21.provenance >/dev/null
grep -F 'sha256=8864abb3b72a6b404445a8c25183c79cfa44a80def0d775ac79578e74fb980e7' deployments/sing-box-1.13.21.provenance >/dev/null
grep -F 'USER nonroot:nonroot' backend/Dockerfile >/dev/null
grep -F 'USER nginx' frontend/Dockerfile >/dev/null
grep -F 'USER 10001:10001' agent/Dockerfile >/dev/null
grep -F 'WORKDIR /src/backend' backend/Dockerfile >/dev/null
grep -F 'go build -trimpath -ldflags='"'"'-s -w'"'"' -o /out/coxpanel-server ./cmd/server' backend/Dockerfile >/dev/null
grep -F 'go build -trimpath -ldflags='"'"'-s -w'"'"' -o /out/coxpanel-bootstrap-owner ./cmd/bootstrap-owner' backend/Dockerfile >/dev/null
grep -F 'user: "70:70"' docker-compose.yml >/dev/null
grep -F 'user: "70:70"' deployments/acceptance/docker-compose.yml >/dev/null
test "$(grep -c 'nocopy: false' docker-compose.yml)" -eq 1
test "$(grep -c 'nocopy: false' deployments/acceptance/docker-compose.yml)" -eq 1
grep -F '/run/postgresql:uid=70,gid=70,mode=0755' docker-compose.yml >/dev/null
grep -F '/run/postgresql:uid=70,gid=70,mode=0755' deployments/acceptance/docker-compose.yml >/dev/null
grep -F 'platform: linux/amd64' deployments/acceptance/docker-compose.yml >/dev/null
grep -F 'read_only: true' docker-compose.yml >/dev/null
grep -F 'cap_drop:' docker-compose.yml >/dev/null
test "$(grep -c '^    read_only: true$' docker-compose.yml)" -eq 3
test "$(grep -c '^    cap_drop:$' docker-compose.yml)" -eq 3
test "$(grep -c '^      - no-new-privileges:true$' docker-compose.yml)" -eq 3
test "$(grep -c '^    read_only: true$' deployments/acceptance/docker-compose.yml)" -eq 5
test "$(grep -c '^    cap_drop:$' deployments/acceptance/docker-compose.yml)" -eq 5
test "$(grep -c '^      - no-new-privileges:true$' deployments/acceptance/docker-compose.yml)" -eq 5
if grep -Eq '^[[:space:]]+(privileged|network_mode|container_name):|/var/run/docker\.sock' docker-compose.yml deployments/acceptance/docker-compose.yml; then
  echo "unsafe host/container privilege setting detected" >&2
  exit 1
fi
grep -F 'access_log off' deployments/nginx/default.conf >/dev/null
grep -F 'COXPANEL_JWT_SECRET: ${COXPANEL_JWT_SECRET:?' docker-compose.yml >/dev/null
grep -F 'location /sub/' deployments/nginx/default.conf >/dev/null
grep -F 'proxy_cache off' deployments/nginx/default.conf >/dev/null
grep -F 'error_log /dev/null emerg' deployments/nginx/default.conf >/dev/null
grep -F 'com.docker.compose.project' deployments/acceptance/down.sh >/dev/null
grep -F 'com.docker.compose.volume' deployments/acceptance/down.sh >/dev/null
grep -F 'com.docker.compose.network' deployments/acceptance/down.sh >/dev/null
grep -F 'bootstrap-owner' deployments/acceptance/down.sh >/dev/null
grep -F 'Docker daemon is required for acceptance cleanup' deployments/acceptance/down.sh >/dev/null
grep -F -- '--user 70:70' deployments/acceptance/run-no-compose.sh >/dev/null
grep -F -- '--env COXPANEL_JWT_SECRET' deployments/acceptance/run-no-compose.sh >/dev/null
grep -F -- '--env COXPANEL_AGENT_CREDENTIAL' deployments/acceptance/run-no-compose.sh >/dev/null
if grep -F 'docker compose' deployments/acceptance/down.sh >/dev/null; then
  echo "acceptance cleanup must not require compose interpolation or secrets" >&2
  exit 1
fi
grep -F 'POSTGRES_HOST_AUTH_METHOD: trust' deployments/acceptance/docker-compose.yml >/dev/null
if grep -F 'POSTGRES_HOST_AUTH_METHOD: trust' docker-compose.yml >/dev/null; then
  echo "production postgres must not use trust authentication" >&2
  exit 1
fi
if grep -Eq 'POSTGRES_PASSWORD:.*:-|COXPANEL_JWT_SECRET:.*:-|change-me|devpass|password123' docker-compose.yml; then
  echo "weak production secret default detected" >&2
  exit 1
fi
if grep -F '**/*credentials*' .dockerignore >/dev/null; then
  echo "dockerignore would omit credential-named source files" >&2
  exit 1
fi
if awk '
/^  postgres:/ { in_postgres=1; next }
/^  [[:alnum:]_-]+:/ { in_postgres=0 }
in_postgres && /^[[:space:]]+ports:/ { found=1 }
END { exit found ? 0 : 1 }
' docker-compose.yml; then
  echo "production postgres must not publish ports" >&2
  exit 1
fi
if awk '
/^  db:/ { in_db=1; next }
/^  [[:alnum:]_-]+:/ { in_db=0 }
in_db && /^[[:space:]]+ports:/ { found=1 }
END { exit found ? 0 : 1 }
' deployments/acceptance/docker-compose.yml; then
  echo "acceptance postgres must not publish ports" >&2
  exit 1
fi
if grep -E '^[[:space:]]+- "[^"]*:[^"]*"' docker-compose.yml deployments/acceptance/docker-compose.yml | grep -v '127\.0\.0\.1:' >/dev/null; then
  echo "published packaging ports must bind to loopback" >&2
  exit 1
fi
test "$(grep -c '^    internal: true$' docker-compose.yml)" -eq 1
test "$(grep -c '^    internal: true$' deployments/acceptance/docker-compose.yml)" -eq 1
sh -n deployments/validate-packaging.sh deployments/acceptance/up.sh deployments/acceptance/down.sh deployments/acceptance/run-no-compose.sh

if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
  : "${COXPANEL_DB_PASSWORD:?COXPANEL_DB_PASSWORD is required for compose config validation}"
  : "${COXPANEL_DB_URL:?COXPANEL_DB_URL is required for compose config validation}"
  : "${COXPANEL_JWT_SECRET:?COXPANEL_JWT_SECRET is required for compose config validation}"
  docker compose -f docker-compose.yml config >/dev/null
  : "${P1_TEST_JWT_SECRET:?P1_TEST_JWT_SECRET is required for acceptance config validation}"
  docker compose -f deployments/acceptance/docker-compose.yml config >/dev/null
else
  echo "docker compose unavailable; performed static packaging checks only" >&2
fi

echo "packaging static checks passed"
