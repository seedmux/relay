FROM --platform=$BUILDPLATFORM golang:1.26 AS build
ARG TARGETOS TARGETARCH VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
    -ldflags "-s -w -X main.version=$VERSION" -o /out/seedmux-relay .

# Runs as root so it can bind 80 / 443 for automatic HTTPS; use --network host
# so the TURN port range does not have to be published one by one.
FROM gcr.io/distroless/static-debian12
COPY --from=build /out/seedmux-relay /usr/local/bin/seedmux-relay
VOLUME /data
ENTRYPOINT ["/usr/local/bin/seedmux-relay", "-data-dir", "/data"]
