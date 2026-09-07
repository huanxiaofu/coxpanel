# Disposable acceptance stack

This compose project is test-only. It must not share a production Docker network, database volume, or credential. The default project name is `coxpanel-p1-acceptance`; `up.sh` and `down.sh` refuse unrelated project names.

## Bring-up

```sh
export P1_TEST_JWT_SECRET="$(openssl rand -hex 32)"
deployments/acceptance/up.sh
```

The database uses passwordless `trust` only inside the internal, project-specific network and is destroyed by `down.sh`. PostgreSQL has no published port. The frontend and static target are published on loopback only.

The checked-in `fixtures` directory contains only the static HTTP target page. Set `P1_TEST_FIXTURES_DIR` to an absolute, test-owned directory when generated protocol fixtures are needed; `up.sh` validates that the directory exists and mounts it read-only into the optional agent only.

When the optional agent profile is enabled, the three documented loopback ports are reserved for test-owned Reality (`18443`), SS2022 (`18388`), and Hysteria2 (`18444/udp`) fixtures. The backend-created listener configuration must use those ports or the coordinator must provide matching overrides.

For an already panel-created managed node, add the optional agent profile with runtime-only values:

```sh
export P1_TEST_WITH_AGENT=1
export P1_TEST_AGENT_NODE_ID='123'
export P1_TEST_AGENT_CREDENTIAL='test-owned-runtime-credential'
deployments/acceptance/up.sh
```

Do not put those values in this repository or pass real credentials. The agent state volume is disposable and removed by `down.sh`.

## Coordinator acceptance order

1. Build the three images with root context and inspect the compose config.
2. Bring up the stack and run `/usr/local/bin/coxpanel-bootstrap-owner` as a one-shot backend container with a runtime-only password file, then create a user, group, managed node, inbounds, and subscription through the real API.
3. Save topology, preview it, and explicitly deploy the returned version; do not treat the redacted preview as a client-side source of truth.
4. Start the optional agent profile and use the pinned sing-box binary plus test-owned Mihomo/client and local target fixtures for protocol and landing-hop checks.
5. Exercise wrong-owner/revoked credentials, stale preview, failed check/start rollback, and offline restart. A JSON check alone is not a handshake.
6. Capture logs without bearer URLs or credentials, then run `deployments/acceptance/down.sh` and verify only the project-prefixed resources were removed.

The agent image is Linux-amd64-only because its build pins the official sing-box `1.13.21` musl archive. The agent's writable state is confined to the named `agentstate` volume; generated protocol fixtures are the only bind mount and are read-only. Set an explicit unique `P1_ACCEPTANCE_PROJECT` such as `coxpanel-p1-acceptance-run-<id>` for concurrent coordinator runs; cleanup accepts only that prefix and removes only that Compose project and its volumes.

The same three root-context `docker build --platform linux/amd64` commands and a no-Compose `docker run` cleanup outline are in `deployments/README.md`. They are coordinator-run instructions, not local execution evidence.

The no-Compose launcher defaults to `https://proxy.golang.org`. A coordinator
may set `P1_BUILD_GOPROXY=https://goproxy.cn,direct` when the default registry
is unreachable; this is an explicit backend build argument and keeps module
readonly/verification checks enabled.

When the coordinator has no Compose plugin, run the reviewed standalone launcher from the repository root:

```sh
export P1_ACCEPTANCE_PROJECT="coxpanel-p1-acceptance-run-<id>"
export P1_TEST_JWT_SECRET="$(openssl rand -hex 32)"
P1_TEST_WITH_AGENT=0 deployments/acceptance/run-no-compose.sh
```

This creates only the project-labeled network/volumes, builds all three root-context images, and starts the database, backend, frontend, and local target. After the backend creates a node, use the coordinator proposal's exact labeled agent `docker run` command; cleanup remains `P1_ACCEPTANCE_PROJECT="$P1_ACCEPTANCE_PROJECT" deployments/acceptance/down.sh`.

## Local final verification

`verify.sh` is the bounded, non-Docker final verification entrypoint. It does
not start or clean up the acceptance stack; the approved disposable PostgreSQL
must already be reachable. Run it from the repository root with the exact
opt-in DSN:

```sh
export P1_TEST_DB_URL='postgres://p1test@coxpanel-p1-testdb:5432/p1test?sslmode=disable'
deployments/acceptance/verify.sh
```

The verifier fails closed when the DSN is absent or different, cached Go
directories/tooling are unavailable, `TMPDIR` or `GOTMPDIR` cannot execute,
or a required binary is missing. It requires the currently supplied pinned
client paths used by the acceptance sources:

- `/work/tools/mihomo`
- `/workspace/tmp/sing-box-audit/sing-box-1.13.21-linux-amd64/sing-box`

It builds the agent from `/workspace/agent`, runs full tests and vet from the
`agent`, `shared`, and `backend` module directories, runs the real repository
race suite and `backend/internal/acceptance` with the opt-in database, runs a
separate malformed-DSN repository test that must exit `1`, and runs frontend
`npm run build` and `npm run lint` from `/workspace/frontend`. The malformed
DSN exit is expected and counts as a successful verification row; an omitted
or invalid opt-in DSN is never treated as a skip. Go `--- SKIP:` output also
fails verification.

The default cached environment is `GO_BIN=/opt/data/go/bin/go`,
`GOWORK=/work/gowork`, `GOFLAGS=-mod=readonly`, `GOPROXY=off`,
`GOMODCACHE=/gomodcache`, `GOCACHE=/work/gocache`, and both temporary
directories `/work/gotmp`. Override `GO_BIN`, `GOWORK`, `GOFLAGS`, `GOPROXY`,
`GOMODCACHE`, `GOCACHE`, `TMPDIR`, `GOTMPDIR`, `NPM_BIN`, `NODE_BIN`, or
`P1_VERIFY_LOG_ROOT` only with test-owned cached paths. Sanitized mode-0600
command logs are written below `/work/p1-final-verification/` by default and
the final report is `/work/p1-final-verification.md`; the report includes
working directories, commands, exits, and SHA-256 hashes for the built agent,
pinned binaries, and verification documents.
