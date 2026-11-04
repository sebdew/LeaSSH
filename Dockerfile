# syntax=docker/dockerfile:1
ARG GO_VERSION=1.26
# Image multi-arch (amd64/arm64) : docker buildx build --platform linux/amd64,linux/arm64 -t leassh .

# --- 1. interface (React) ---
FROM --platform=$BUILDPLATFORM node:20-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# --- 2. binaire Go (pur Go, sans CGO) ---
# go.mod exige Go 1.26 ; GOTOOLCHAIN=auto télécharge la version exacte si l'image est plus ancienne.
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS build
ENV GOTOOLCHAIN=auto
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
ARG TARGETOS TARGETARCH
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/leassh ./cmd/leassh \
 && mkdir /out/data

# --- 3. image finale : distroless, utilisateur non-root ---
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/leassh /leassh
COPY --from=build --chown=65532:65532 /out/data /data
ENV LEASSH_DATA_DIR=/data LEASSH_LISTEN=:8080
VOLUME /data
EXPOSE 8080
USER 65532:65532
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s CMD ["/leassh", "healthcheck"]
ENTRYPOINT ["/leassh"]
CMD ["serve"]
