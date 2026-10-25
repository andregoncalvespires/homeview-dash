# syntax=docker/dockerfile:1

# ---- build: static binary (pure Go SQLite driver, no CGO) ----
FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY . .
# go.sum is normally committed; if it is missing (first build), resolve it now.
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    ( [ -f go.sum ] || go mod tidy ) && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/homeview ./cmd/homeview

# ---- runtime ----
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata \
 && adduser -D -H -u 10001 homeview \
 && mkdir /data && chown homeview /data
COPY --from=build /out/homeview /usr/local/bin/homeview
USER homeview
ENV HOMEVIEW_ADDR=:8080 HOMEVIEW_DATA=/data
VOLUME /data
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["homeview", "healthcheck"]
ENTRYPOINT ["homeview"]
