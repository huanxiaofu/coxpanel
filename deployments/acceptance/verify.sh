#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
report=/work/p1-final-verification.md
log_root=${P1_VERIFY_LOG_ROOT:-/work/p1-final-verification}
approved_db_url='postgres://p1test@coxpanel-p1-testdb:5432/p1test?sslmode=disable'
script_path="$root/deployments/acceptance/verify.sh"
readme_path="$root/deployments/acceptance/README.md"

case "$log_root" in
  /*) ;;
  *) printf '%s\n' 'P1_VERIFY_LOG_ROOT must be an absolute path' >&2; exit 2 ;;
esac
mkdir -p "$log_root"
chmod 700 "$log_root"
run_stamp=$(date -u +%Y%m%dT%H%M%SZ)
run_dir="$log_root/$run_stamp"
if [[ -e "$run_dir" ]]; then
  run_dir="$log_root/${run_stamp}-$$"
fi
mkdir -p "$run_dir"
chmod 700 "$run_dir"
records="$run_dir/results.tsv"
preflight_log="$run_dir/preflight.log"
: > "$records"
: > "$preflight_log"
chmod 600 "$records" "$preflight_log"

failure_reason=''
last_log=''

sanitize() {
  LC_ALL=C sed -E \
    -e 's#(postgres(ql)?://)[^[:space:]]+#\1<redacted-dsn>#Ig' \
    -e 's#(Authorization:[[:space:]]*Bearer[[:space:]]+)[^[:space:]]+#\1<redacted>#Ig' \
    -e 's#(Bearer[[:space:]]+)[^[:space:]]+#\1<redacted>#Ig' \
    -e 's#([?&](token|key|secret|credential|password|api[_-]?key|access_token)=)[^&[:space:]]+#\1<redacted>#Ig' \
    -e 's#((password|passwd|token|secret|credential|api[_-]?key|authorization|bearer)[[:space:]]*[:=][[:space:]]*)("[^"]*"|[^[:space:],}]+)#\1<redacted>#Ig' \
    -e 's#(P1_TEST_(AGENT_CREDENTIAL|JWT_SECRET)=)[^[:space:]]+#\1<redacted>#g' \
    -e 's#(COXPANEL_(AGENT_CREDENTIAL|JWT_SECRET)=)[^[:space:]]+#\1<redacted>#g'
}

write_report() {
  local exit_status=${1:-1}
  local result=PASS
  local label cwd display status log
  if [[ "$exit_status" -ne 0 || -n "$failure_reason" ]]; then
    result=FAIL
  fi
  {
    printf '%s\n\n' '# P1 final integrated verification'
    printf '%s\n' "- result: $result"
    printf '%s\n' "- repository: $root"
    printf '%s\n' "- started_utc: $run_stamp"
    printf '%s\n' "- verifier_exit: $exit_status"
    printf '%s\n' "- log_directory: $run_dir"
    if [[ -n "$failure_reason" ]]; then
      printf '%s\n' "- failure: $failure_reason"
    fi
    printf '%s\n\n' '- Database mode: the approved disposable PostgreSQL DSN was required; logs redact DSNs and credentials.'
    printf '%s\n' '## Preflight'
    printf '%s\n\n' "- log: $preflight_log"
    printf '%s\n' '```text'
    sanitize < "$preflight_log"
    printf '%s\n\n' '```'
    printf '%s\n' '## Commands'
    if [[ -s "$records" ]]; then
      while IFS=$'\t' read -r label cwd display status log; do
        printf '%s\n' "- $label: cwd=$cwd; command=$display; exit=$status; log=$log"
      done < "$records"
    fi
    printf '%s\n' '## Artifact hashes'
    if [[ -n "$last_log" && -f "$last_log" ]]; then
      printf '%s\n' "- log: $last_log"
      printf '%s\n' '```text'
      sanitize < "$last_log"
      printf '%s\n' '```'
    else
      printf '%s\n' '- not reached'
    fi
  } > "$run_dir/report.md"
  chmod 600 "$run_dir/report.md"
  mv -f "$run_dir/report.md" "$report"
  chmod 600 "$report"
}

on_exit() {
  local exit_status=$?
  trap - EXIT
  write_report "$exit_status"
  exit "$exit_status"
}
trap on_exit EXIT

resolve_executable() {
  local requested=$1
  local resolved
  if [[ "$requested" == */* ]]; then
    if [[ ! -f "$requested" || ! -x "$requested" ]]; then
      failure_reason="$2 is not executable: $requested"
      return 1
    fi
    printf '%s\n' "$requested"
    return 0
  fi
  resolved=$(command -v "$requested" || true)
  if [[ -z "$resolved" || ! -x "$resolved" ]]; then
    failure_reason="$2 is not available: $requested"
    return 1
  fi
  printf '%s\n' "$resolved"
}

go_requested=${GO_BIN:-/opt/data/go/bin/go}
npm_requested=${NPM_BIN:-npm}
node_requested=${NODE_BIN:-node}
go_bin=$(resolve_executable "$go_requested" Go)
npm_bin=$(resolve_executable "$npm_requested" npm)
node_bin=$(resolve_executable "$node_requested" Node)
sha256sum_bin=$(resolve_executable sha256sum sha256sum)

gowork=${GOWORK-/work/gowork}
goflags=${GOFLAGS--mod=readonly}
goproxy=${GOPROXY-off}
gomodcache=${GOMODCACHE-/gomodcache}
gocache=${GOCACHE-/work/gocache}
tmpdir=${TMPDIR-/work/gotmp}
gotmpdir=${GOTMPDIR-$tmpdir}

if [[ "${P1_TEST_DB_URL-}" != "$approved_db_url" ]]; then
  failure_reason='P1_TEST_DB_URL must equal the approved disposable PostgreSQL DSN'
  exit 1
fi

export GOWORK="$gowork"
export GOFLAGS="$goflags"
export GOPROXY="$goproxy"
export GOMODCACHE="$gomodcache"
export GOCACHE="$gocache"
export TMPDIR="$tmpdir"
export GOTMPDIR="$gotmpdir"
export P1_TEST_DB_URL="$approved_db_url"

printf '%s\n' "root=$root" >> "$preflight_log"
printf '%s\n' "approved_db_url=present" >> "$preflight_log"
printf '%s\n' "GO_BIN=$go_bin" >> "$preflight_log"
printf '%s\n' "NPM_BIN=$npm_bin" >> "$preflight_log"
printf '%s\n' "NODE_BIN=$node_bin" >> "$preflight_log"
printf '%s\n' "GOWORK=$gowork" >> "$preflight_log"
printf '%s\n' "GOFLAGS=$goflags" >> "$preflight_log"
if [[ "$goproxy" == 'off' ]]; then
  printf '%s\n' 'GOPROXY=off' >> "$preflight_log"
else
  printf '%s\n' 'GOPROXY=<redacted>' >> "$preflight_log"
fi
printf '%s\n' "GOMODCACHE=$gomodcache" >> "$preflight_log"
printf '%s\n' "GOCACHE=$gocache" >> "$preflight_log"
printf '%s\n' "TMPDIR=$tmpdir" >> "$preflight_log"
printf '%s\n' "GOTMPDIR=$gotmpdir" >> "$preflight_log"

require_directory() {
  local name=$1 path=$2 writable=${3:-0}
  if [[ ! -d "$path" || ! -r "$path" || ! -x "$path" ]]; then
    failure_reason="$name must be an existing readable/executable directory: $path"
    return 1
  fi
  if [[ "$writable" == 1 && ! -w "$path" ]]; then
    failure_reason="$name must be writable: $path"
    return 1
  fi
  printf '%s\n' "PASS $name" >> "$preflight_log"
}

require_executable_file() {
  local name=$1 path=$2
  if [[ ! -f "$path" || ! -x "$path" ]]; then
    failure_reason="$name must be an executable regular file: $path"
    return 1
  fi
  printf '%s\n' "PASS $name" >> "$preflight_log"
}

require_directory TMPDIR "$tmpdir" 1
require_directory GOTMPDIR "$gotmpdir" 1
require_directory GOCACHE "$gocache" 1
require_directory GOMODCACHE "$gomodcache" 0
if [[ "$gowork" == 'off' ]]; then
  printf '%s\n' 'PASS GOWORK=off' >> "$preflight_log"
elif [[ -f "$gowork" && -r "$gowork" ]]; then
  printf '%s\n' 'PASS GOWORK' >> "$preflight_log"
else
  failure_reason="GOWORK must name a readable go.work file or be off: $gowork"
  exit 1
fi

probe_exec_dir() {
  local name=$1 path=$2 probe_dir probe
  probe_dir=$(mktemp -d "$path/p1-verify-exec.XXXXXX") || {
    failure_reason="$name could not create an execution probe"
    return 1
  }
  probe="$probe_dir/probe"
  printf '%s\n' '#!/bin/sh' 'exit 0' > "$probe"
  chmod 700 "$probe"
  if "$probe"; then
    printf '%s\n' "PASS $name executable" >> "$preflight_log"
  else
    rm -rf "$probe_dir"
    failure_reason="$name does not permit execution"
    return 1
  fi
  rm -rf "$probe_dir"
}
probe_exec_dir TMPDIR "$tmpdir"
probe_exec_dir GOTMPDIR "$gotmpdir"

const_mihomo=/work/tools/mihomo
const_singbox=/workspace/tmp/sing-box-audit/sing-box-1.13.21-linux-amd64/sing-box
require_executable_file 'pinned Mihomo binary' "$const_mihomo"
require_executable_file 'pinned sing-box binary' "$const_singbox"
require_executable_file 'verification script' "$script_path"

node_bin_dir=$(dirname -- "$node_bin")
export PATH="$node_bin_dir:$PATH"

quote_for_display() {
  printf '%q' "$1"
}

safe_proxy_display='<redacted>'
if [[ "$goproxy" == 'off' ]]; then
  safe_proxy_display=off
fi
go_display_prefix="env GOWORK=$(quote_for_display "$gowork") GOFLAGS=$(quote_for_display "$goflags") GOPROXY=$safe_proxy_display GOMODCACHE=$(quote_for_display "$gomodcache") GOCACHE=$(quote_for_display "$gocache") TMPDIR=$(quote_for_display "$tmpdir") GOTMPDIR=$(quote_for_display "$gotmpdir")"
go_display_db="$go_display_prefix P1_TEST_DB_URL=<approved-disposable>"

record_step() {
  local label=$1 cwd=$2 expected=$3 display=$4
  shift 4
  local log="$run_dir/$label.log"
  last_log="$log"
  {
    printf '%s\n' "cwd=$cwd"
    printf '%s\n' "command=$display"
  } > "$log"
  set +e
  (cd "$cwd" && "$@") 2>&1 | sanitize >> "$log"
  local command_status=${PIPESTATUS[0]}
  set -e
  printf '%s\n' "exit=$command_status" >> "$log"
  chmod 600 "$log"
  printf '%s\t%s\t%s\t%s\t%s\n' "$label" "$cwd" "$display" "$command_status" "$log" >> "$records"
  printf '%s\n' "[verify] $label exit=$command_status"
  if [[ "$command_status" -ne "$expected" ]]; then
    failure_reason="$label exited $command_status; expected $expected"
    return 1
  fi
  if grep -Eq '^[[:space:]]*--- SKIP:' "$log"; then
    failure_reason="$label reported a skipped test"
    return 1
  fi
}

record_step toolchain-go "$root" 0 "$go_bin version" "$go_bin" version
record_step toolchain-node "$root" 0 "$node_bin --version" "$node_bin" --version
record_step toolchain-npm "$root" 0 "$npm_bin --version" "$npm_bin" --version

agent_binary="$run_dir/coxpanel-agent"
record_step agent-build "$root/agent" 0 "$go_display_prefix $go_bin build -trimpath -o $(quote_for_display "$agent_binary") ./cmd/agent" "$go_bin" build -trimpath -o "$agent_binary" ./cmd/agent
require_executable_file 'built agent binary' "$agent_binary"
export P1_TEST_AGENT_BINARY="$agent_binary"

record_step agent-test "$root/agent" 0 "$go_display_prefix $go_bin test -v ./... -count=1" "$go_bin" test -v ./... -count=1
record_step agent-vet "$root/agent" 0 "$go_display_prefix $go_bin vet ./..." "$go_bin" vet ./...

record_step shared-test "$root/shared" 0 "$go_display_prefix $go_bin test -v ./... -count=1" "$go_bin" test -v ./... -count=1
record_step shared-vet "$root/shared" 0 "$go_display_prefix $go_bin vet ./..." "$go_bin" vet ./...

record_step backend-test "$root/backend" 0 "$go_display_db $go_bin test -v ./... -count=1" "$go_bin" test -v ./... -count=1
record_step backend-vet "$root/backend" 0 "$go_display_db $go_bin vet ./..." "$go_bin" vet ./...
record_step backend-repo-race "$root/backend" 0 "$go_display_db $go_bin test -race -v ./internal/repo -count=1" "$go_bin" test -race -v ./internal/repo -count=1
record_step backend-acceptance "$root/backend" 0 "$go_display_db P1_TEST_AGENT_BINARY=$(quote_for_display "$agent_binary") $go_bin test -v ./internal/acceptance -count=1" "$go_bin" test -v ./internal/acceptance -count=1

negative_log="$run_dir/backend-bad-dsn.log"
record_step backend-bad-dsn "$root/backend" 1 "$go_display_prefix P1_TEST_DB_URL=not-a-database-url $go_bin test -v ./internal/repo -run '^TestRegisterWithInviteConcurrentIntegration$' -count=1" env P1_TEST_DB_URL=not-a-database-url "$go_bin" test -v ./internal/repo -run '^TestRegisterWithInviteConcurrentIntegration$' -count=1
if ! grep -Fq 'P1_TEST_DB_URL is invalid' "$negative_log"; then
  failure_reason='backend-bad-dsn did not fail at explicit DSN validation'
  exit 1
fi

record_step frontend-build "$root/frontend" 0 "$npm_bin run build" "$npm_bin" run build
record_step frontend-lint "$root/frontend" 0 "$npm_bin run lint" "$npm_bin" run lint

hash_log="$run_dir/artifact-hashes.log"
last_log="$hash_log"
{
  printf '%s\n' "cwd=$root"
  printf '%s\n' "command=$sha256sum_bin $agent_binary $const_mihomo $const_singbox $script_path $readme_path"
} > "$hash_log"
set +e
(cd "$root" && "$sha256sum_bin" "$agent_binary" "$const_mihomo" "$const_singbox" "$script_path" "$readme_path") 2>&1 | sanitize >> "$hash_log"
hash_status=${PIPESTATUS[0]}
set -e
printf '%s\n' "exit=$hash_status" >> "$hash_log"
chmod 600 "$hash_log"
printf '%s\t%s\t%s\t%s\t%s\n' artifact-hashes "$root" "$sha256sum_bin $agent_binary $const_mihomo $const_singbox $script_path $readme_path" "$hash_status" "$hash_log" >> "$records"
printf '%s\n' "[verify] artifact-hashes exit=$hash_status"
if [[ "$hash_status" -ne 0 ]]; then
  failure_reason="artifact-hashes exited $hash_status"
  exit 1
fi

printf '%s\n' "[verify] PASS; evidence=$report; logs=$run_dir"
