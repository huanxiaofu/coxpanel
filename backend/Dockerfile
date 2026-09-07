FROM golang:1.26.8-alpine AS build

RUN apk add --no-cache ca-certificates

WORKDIR /src
COPY shared ./shared
COPY backend/go.mod backend/go.sum ./backend/

WORKDIR /src/backend
ARG GOPROXY=https://proxy.golang.org
ENV GOWORK=off \
    GOFLAGS=-mod=readonly \
    GOTOOLCHAIN=local \
    GOPROXY=${GOPROXY}
RUN go mod download && go mod verify

WORKDIR /src
COPY backend ./backend
WORKDIR /src/backend
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/coxpanel-server ./cmd/server
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/coxpanel-bootstrap-owner ./cmd/bootstrap-owner

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/coxpanel-server /usr/local/bin/coxpanel-server
COPY --from=build /out/coxpanel-bootstrap-owner /usr/local/bin/coxpanel-bootstrap-owner

USER nonroot:nonroot
EXPOSE 8080
STOPSIGNAL SIGTERM
ENTRYPOINT ["/usr/local/bin/coxpanel-server"]
