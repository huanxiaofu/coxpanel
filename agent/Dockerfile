FROM golang:1.26.8-alpine AS build

WORKDIR /src
COPY shared ./shared
COPY agent/go.mod ./agent/go.mod
COPY agent ./agent

WORKDIR /src/agent
ENV GOWORK=off \
    GOFLAGS=-mod=readonly \
    GOTOOLCHAIN=local \
    GOPROXY=off
RUN go mod verify && CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/coxpanel-agent ./cmd/agent

FROM alpine:3.21 AS singbox

ARG SING_BOX_VERSION=1.13.21
ARG SING_BOX_ARCHIVE_SHA256=8864abb3b72a6b404445a8c25183c79cfa44a80def0d775ac79578e74fb980e7

COPY deployments/sing-box-1.13.21.provenance /tmp/sing-box-1.13.21.provenance

RUN set -eux; \
    test "${SING_BOX_VERSION}" = "1.13.21"; \
    test "${SING_BOX_ARCHIVE_SHA256}" = "8864abb3b72a6b404445a8c25183c79cfa44a80def0d775ac79578e74fb980e7"; \
    provenance_artifact="$(awk -F= '$1 == "artifact" { print $2 }' /tmp/sing-box-1.13.21.provenance)"; \
    provenance_download="$(awk -F= '$1 == "download" { print $2 }' /tmp/sing-box-1.13.21.provenance)"; \
    provenance_release_tag="$(awk -F= '$1 == "release_tag" { print $2 }' /tmp/sing-box-1.13.21.provenance)"; \
    provenance_immutable="$(awk -F= '$1 == "release_immutable" { print $2 }' /tmp/sing-box-1.13.21.provenance)"; \
    provenance_asset_id="$(awk -F= '$1 == "asset_id" { print $2 }' /tmp/sing-box-1.13.21.provenance)"; \
    provenance_sha256="$(awk -F= '$1 == "sha256" { print $2 }' /tmp/sing-box-1.13.21.provenance)"; \
    provenance_size="$(awk -F= '$1 == "asset_size" { print $2 }' /tmp/sing-box-1.13.21.provenance)"; \
    test "${provenance_artifact}" = "sing-box-${SING_BOX_VERSION}-linux-amd64-musl.tar.gz"; \
    test "${provenance_download}" = "https://github.com/SagerNet/sing-box/releases/download/v${SING_BOX_VERSION}/sing-box-${SING_BOX_VERSION}-linux-amd64-musl.tar.gz"; \
    test "${provenance_release_tag}" = "v${SING_BOX_VERSION}"; \
    test "${provenance_immutable}" = "true"; \
    test "${provenance_asset_id}" = "536461728"; \
    test "${provenance_sha256}" = "${SING_BOX_ARCHIVE_SHA256}"; \
    test -n "${provenance_size}"; \
    test "$(apk --print-arch)" = "x86_64"; \
    apk add --no-cache ca-certificates curl tar; \
    curl --fail --location --proto '=https' --tlsv1.2 --retry 3 \
      "https://github.com/SagerNet/sing-box/releases/download/v${SING_BOX_VERSION}/sing-box-${SING_BOX_VERSION}-linux-amd64-musl.tar.gz" \
      --output /tmp/sing-box.tar.gz; \
    echo "${SING_BOX_ARCHIVE_SHA256}  /tmp/sing-box.tar.gz" | sha256sum -c -; \
    test "$(wc -c < /tmp/sing-box.tar.gz)" -eq "${provenance_size}"; \
    mkdir -p /tmp/sing-box; \
    tar -xzf /tmp/sing-box.tar.gz -C /tmp/sing-box; \
    sing_box_binary="$(tar -tzf /tmp/sing-box.tar.gz | awk '/(^|\/)sing-box$/ { print; count++ } END { if (count != 1) exit 1 }')"; \
    test -n "${sing_box_binary}"; \
    sing_box_binary="/tmp/sing-box/${sing_box_binary}"; \
    test -x "${sing_box_binary}"; \
    version_output="$("${sing_box_binary}" version)"; \
    printf '%s\n' "${version_output}" | grep -Fx "sing-box version ${SING_BOX_VERSION}" >/dev/null; \
    install -Dm755 "${sing_box_binary}" /usr/local/bin/sing-box

FROM alpine:3.21

RUN apk add --no-cache ca-certificates && \
    addgroup -S -g 10001 coxpanel && \
    adduser -S -D -H -u 10001 -G coxpanel coxpanel && \
    mkdir -p /var/lib/coxpanel && \
    chown -R 10001:10001 /var/lib/coxpanel

COPY --from=build /out/coxpanel-agent /usr/local/bin/coxpanel-agent
COPY --from=singbox /usr/local/bin/sing-box /usr/local/bin/sing-box

RUN /usr/local/bin/sing-box version | head -n 1 | grep -Fx "sing-box version 1.13.21"

USER 10001:10001
WORKDIR /var/lib/coxpanel
VOLUME ["/var/lib/coxpanel"]
STOPSIGNAL SIGTERM
CMD ["-config", "/var/lib/coxpanel/config.json"]
ENTRYPOINT ["/usr/local/bin/coxpanel-agent"]
