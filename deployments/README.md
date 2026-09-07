# CoxPanel deployment material

The root `docker-compose.yml` is a panel deployment example. It uses the repository root as the build context so `backend` and `agent` can compile against `shared` without copying a second contract implementation.

## Panel stack

Generate and inject secrets at runtime. The compose file has no secret defaults and does not publish PostgreSQL:

```sh
export COXPANEL_DB_PASSWORD="$(openssl rand -hex 24)"
export COXPANEL_DB_URL="postgres://coxpanel:${COXPANEL_DB_PASSWORD}@postgres:5432/coxpanel?sslmode=disable"
export COXPANEL_JWT_SECRET="$(openssl rand -hex 32)"
docker compose config
docker compose up --build -d
```

Use a secret manager or an environment injection mechanism for real deployments; do not commit those exports or a `.env` file. The only published service is the frontend, bound to loopback by default at `http://127.0.0.1:18080`. Nginx serves SPA routes and forwards `/api/` and `/sub/` without access logs or cache storage, so subscription bearer URLs are not written to its access log.

After the database is healthy, provision the first owner with the image's one-shot command. Supply the password through the command's existing direct or password-file environment input; do not put it in command arguments or source files:

```sh
export COXPANEL_BOOTSTRAP_USERNAME='owner'
export COXPANEL_BOOTSTRAP_EMAIL='owner@example.test'
export COXPANEL_BOOTSTRAP_PASSWORD="A$(openssl rand -hex 24)!a9"
docker compose run --rm --no-deps \
  -e COXPANEL_BOOTSTRAP_USERNAME \
  -e COXPANEL_BOOTSTRAP_EMAIL \
  -e COXPANEL_BOOTSTRAP_PASSWORD \
  --entrypoint /usr/local/bin/coxpanel-bootstrap-owner backend
unset COXPANEL_BOOTSTRAP_PASSWORD COXPANEL_BOOTSTRAP_USERNAME COXPANEL_BOOTSTRAP_EMAIL
```

The command migrates the database and atomically refuses to create a second owner. The backend image contains both `/usr/local/bin/coxpanel-server` and `/usr/local/bin/coxpanel-bootstrap-owner`; it does not enable an implicit bootstrap endpoint or a default account.

The agent image is built separately because node core listener ports and node credentials are deployment-specific. It is pinned to sing-box `1.13.21` for Linux amd64; the archive digest and installed version are checked in `deployments/sing-box-1.13.21.provenance` and during the image build. Run it with `--platform linux/amd64`, provide `COXPANEL_AGENT_CREDENTIAL` at runtime, and mount a test/deployment-owned writable state directory at `/var/lib/coxpanel`. Do not put credentials in image layers, command arguments, or source files.

## Disposable acceptance stack

`deployments/acceptance/docker-compose.yml` is intentionally separate. It uses a passwordless PostgreSQL trust configuration only on an isolated test network, a project-prefixed disposable volume, loopback-only test ports, and a read-only test target fixture. It is not a production template.

```sh
export P1_TEST_JWT_SECRET="$(openssl rand -hex 32)"
deployments/acceptance/up.sh
deployments/acceptance/down.sh
```

The optional agent profile additionally requires runtime-only `P1_TEST_AGENT_CREDENTIAL` and `P1_TEST_AGENT_NODE_ID`; set `P1_TEST_WITH_AGENT=1` when the backend-created node is ready. The acceptance harness remains responsible for owner/user/group/node/inbounds/topology/subscription creation, real Mihomo traffic, protocol checks, rollback/offline checks, and cleanup assertions. This packaging increment does not claim those end-to-end checks.

Run `deployments/validate-packaging.sh` for local shell/static checks. A Docker daemon and Compose plugin are required for image builds, `docker compose config`, stack bring-up, and cleanup; this workspace intentionally does not have those capabilities. The coordinator proposal records equivalent reviewed `docker build`/`docker run` commands and the acceptance evidence it must capture.

If the coordinator host has the Docker CLI but no Compose plugin, use the exact root-context builds below. Keep `P1_ACCEPTANCE_PROJECT` unique and supply secrets only through the coordinator's external environment mechanism; the example never embeds a secret value:

```sh
export P1_ACCEPTANCE_PROJECT="coxpanel-p1-acceptance-run-<id>"
docker network create --internal \
  --label "com.docker.compose.project=${P1_ACCEPTANCE_PROJECT}" \
  --label com.docker.compose.network=default \
  "${P1_ACCEPTANCE_PROJECT}_default"
docker volume create \
  --label "com.docker.compose.project=${P1_ACCEPTANCE_PROJECT}" \
  --label com.docker.compose.volume=dbdata \
  "${P1_ACCEPTANCE_PROJECT}_dbdata"
docker volume create \
  --label "com.docker.compose.project=${P1_ACCEPTANCE_PROJECT}" \
  --label com.docker.compose.volume=agentstate \
  "${P1_ACCEPTANCE_PROJECT}_agentstate"
docker build --platform linux/amd64 -f backend/Dockerfile -t "${P1_ACCEPTANCE_PROJECT}/backend:review" .
docker build --platform linux/amd64 -f frontend/Dockerfile -t "${P1_ACCEPTANCE_PROJECT}/frontend:review" .
docker build --platform linux/amd64 -f agent/Dockerfile -t "${P1_ACCEPTANCE_PROJECT}/agent:review" .
```

The backend build defaults to `https://proxy.golang.org`. If that registry is
unreachable from the coordinator, opt into a reviewed module mirror explicitly
for the backend source build; the Dockerfile still uses `-mod=readonly` and
runs `go mod verify`:

```sh
docker build --platform linux/amd64 --build-arg GOPROXY=https://goproxy.cn,direct -f backend/Dockerfile -t "${P1_ACCEPTANCE_PROJECT}/backend:review" .
```

When Compose is unavailable, `deployments/acceptance/run-no-compose.sh` is the reviewed standalone launcher. It builds all three images, starts PostgreSQL first, waits for its health state, and starts the backend, frontend, and local target with exact project labels. Set `P1_TEST_WITH_AGENT=0` until the backend has created the managed node; then use the proposal's exact labeled agent command with the generated node ID. The coordinator must start PostgreSQL first with an exact `--name "${P1_ACCEPTANCE_PROJECT}_db-1"`, `--network "${P1_ACCEPTANCE_PROJECT}_default"`, `--network-alias db`, named `dbdata` volume, read-only root, `/run/postgresql` and `/tmp` tmpfs, `--cap-drop ALL`, and `--security-opt no-new-privileges:true`; then start backend and frontend with the same project-prefixed network/name convention and runtime-only environment injection. The optional agent uses `--name "${P1_ACCEPTANCE_PROJECT}_agent-1"`, the project-prefixed `agentstate` volume, a read-only generated-fixtures bind mount, `--read-only`, `--cap-drop ALL`, `--security-opt no-new-privileges:true`, and only loopback-published test listener ports. Cleanup is exact and label-checked by `deployments/acceptance/down.sh`; it removes only the expected project resources after verifying `com.docker.compose.project`, service/volume, and network labels. Do not use `docker prune`.

Set `P1_BUILD_GOPROXY` only when the default proxy is unavailable. The launcher
passes that value as the backend Docker build's explicit `GOPROXY` build
argument; it does not alter module versions or the coordinator proposal.
