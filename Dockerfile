# syntax=docker/dockerfile:1
# Fluid Proxmox execution agent — see code/actions/templates/Dockerfile.go-workload

FROM golang:1.23-bookworm AS build

ARG BINARY_NAME=fluid-agent-proxmox
ARG VERSION=0.0.0
ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /src

COPY go.mod go.sum ./
COPY core ./core
COPY cmd ./cmd
COPY internal ./internal

RUN go mod download

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags "-s -w -X main.Version=${VERSION}" \
        -o /out/workload ./cmd

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/workload /usr/local/bin/workload

USER nonroot:nonroot

ENTRYPOINT ["/usr/local/bin/workload"]
CMD ["-config", "/etc/fluid/config/config.yaml"]
