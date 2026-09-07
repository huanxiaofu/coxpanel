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
docker info >/dev/null 2>&1 || {
  echo "Docker daemon is required for acceptance cleanup" >&2
  exit 1
}

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
network="${project}_default"
db_volume="${project}_dbdata"
agent_volume="${project}_agentstate"
bootstrap_container="${project}-bootstrap-owner-1"

owned_containers=
owned_volumes=
owned_network=

check_container() {
  name=$1
  service=$2
  if ! docker inspect "$name" >/dev/null 2>&1; then
    return 0
  fi
  actual_name=$(docker inspect --format '{{.Name}}' "$name")
  project_label=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project"}}' "$name")
  service_label=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.service"}}' "$name")
  if [ "$actual_name" != "/$name" ] || [ "$project_label" != "$project" ] || [ "$service_label" != "$service" ]; then
    echo "refusing unowned acceptance container: $name" >&2
    return 1
  fi
  owned_containers="$owned_containers $name"
}

check_volume() {
  name=$1
  volume_label=$2
  if ! docker volume inspect "$name" >/dev/null 2>&1; then
    return 0
  fi
  actual_name=$(docker volume inspect --format '{{.Name}}' "$name")
  project_label=$(docker volume inspect --format '{{index .Labels "com.docker.compose.project"}}' "$name")
  actual_volume_label=$(docker volume inspect --format '{{index .Labels "com.docker.compose.volume"}}' "$name")
  if [ "$actual_name" != "$name" ] || [ "$project_label" != "$project" ] || [ "$actual_volume_label" != "$volume_label" ]; then
    echo "refusing unowned acceptance volume: $name" >&2
    return 1
  fi
  owned_volumes="$owned_volumes $name"
}

check_network() {
  if ! docker network inspect "$network" >/dev/null 2>&1; then
    return 0
  fi
  actual_name=$(docker network inspect --format '{{.Name}}' "$network")
  project_label=$(docker network inspect --format '{{index .Labels "com.docker.compose.project"}}' "$network")
  network_label=$(docker network inspect --format '{{index .Labels "com.docker.compose.network"}}' "$network")
  if [ "$actual_name" != "$network" ] || [ "$project_label" != "$project" ] || [ "$network_label" != "default" ]; then
    echo "refusing unowned acceptance network: $network" >&2
    return 1
  fi
  owned_network=1
}

check_container "$bootstrap_container" bootstrap-owner
check_container "${project}-agent-1" agent
check_container "${project}-target-1" target
check_container "${project}-frontend-1" frontend
check_container "${project}-backend-1" backend
check_container "${project}-db-1" db
check_volume "$agent_volume" agentstate
check_volume "$db_volume" dbdata
check_network

for container in $owned_containers; do
  docker rm -f "$container" >/dev/null
done
for volume in $owned_volumes; do
  docker volume rm "$volume" >/dev/null
done
if [ -n "$owned_network" ]; then
  docker network rm "$network" >/dev/null
fi
