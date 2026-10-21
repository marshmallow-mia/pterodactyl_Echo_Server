# Echo VR server image for Pterodactyl/Pelican: the evr-docker base image plus
# the Wings entrypoint and evr-ptero (used by the egg's install script).
ARG BASE=ghcr.io/marshmallow-mia/echovr-server:edge

FROM golang:1.27-trixie AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/evr-ptero ./cmd/evr-ptero

FROM ${BASE}
USER root
RUN apt-get update && apt-get install -y --no-install-recommends rsync curl && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/evr-ptero /usr/local/bin/evr-ptero
COPY image/entrypoint.sh /entrypoint.sh
COPY LICENSE /usr/share/doc/evr-ptero/LICENSE
USER container
WORKDIR /home/container
ENTRYPOINT ["/usr/bin/tini", "--", "/bin/bash", "/entrypoint.sh"]
