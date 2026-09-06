# syntax=docker/dockerfile:1.7

FROM node:22-alpine AS web-build
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26.6-alpine AS cloud-build
WORKDIR /src/cloud
COPY cloud/go.mod cloud/go.sum ./
RUN go mod download
COPY cloud/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/mindfs-relay ./cmd/mindfs-relay
RUN mkdir -p /out/data /out/backups /out/assets

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=cloud-build /out/mindfs-relay /usr/local/bin/mindfs-relay
COPY --from=web-build /src/web/dist /opt/mindfs/web
COPY --chown=65532:65532 --from=cloud-build /out/data /var/lib/mindfs-cloud
COPY --chown=65532:65532 --from=cloud-build /out/backups /backups
COPY --chown=65532:65532 --from=cloud-build /out/assets /var/lib/mindfs-assets
ENV MINDFS_CLOUD_ADDR=:8080 \
    MINDFS_CLOUD_DATA_DIR=/var/lib/mindfs-cloud \
    MINDFS_CLOUD_ASSETS_DIR=/opt/mindfs/web
USER 65532:65532
VOLUME ["/var/lib/mindfs-cloud", "/var/lib/mindfs-assets", "/backups"]
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["/usr/local/bin/mindfs-relay", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/mindfs-relay"]
CMD ["serve"]
