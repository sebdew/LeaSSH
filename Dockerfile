# syntax=docker/dockerfile:1
ARG GO_VERSION=1.26
# Image multi-arch (amd64/arm64) : docker buildx build --platform linux/amd64,linux/arm64 -t leassh .

# --- 1. interface (React) ---
# Node 24 LTS (support jusqu'en avril 2028). Images figées par condensat: la mise à jour est volontaire (voir docs/INSTALL.md).
FROM --platform=$BUILDPLATFORM node:24-alpine@sha256:a34ab6d7f6f4813f4d0c5469d74685604b962fbdbf5793a7bc7b4998ecb867c8 AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# --- 2. binaire Go (pur Go, sans CGO) ---
# go.mod exige Go 1.26 ; GOTOOLCHAIN=auto télécharge la version exacte si l'image est plus ancienne.
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine@sha256:87c65a688975c0b1d4585d204874c45ca78354670e3a5f11368c5a492303d696 AS build
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
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /out/leassh /leassh
COPY --from=build --chown=65532:65532 /out/data /data
ENV LEASSH_DATA_DIR=/data LEASSH_LISTEN=:8080
VOLUME /data
EXPOSE 8080
USER 65532:65532
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s CMD ["/leassh", "healthcheck"]
ENTRYPOINT ["/leassh"]
CMD ["serve"]
