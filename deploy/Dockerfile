# ROADPASS service image: the dashboard is built with Node, embedded into the Go
# binary, and shipped on distroless as a non-root user.
FROM node:24-bookworm-slim AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web ./
RUN npx tsc -b && npx vite build --outDir /dist --emptyOutDir

FROM golang:1.27-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN rm -rf internal/api/dist
COPY --from=web /dist ./internal/api/dist
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/roadpass ./cmd/roadpass

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/roadpass /roadpass
ENV HTTP_ADDR=:8080 LOG_FORMAT=json
EXPOSE 8080
USER nonroot:nonroot
HEALTHCHECK --interval=15s --timeout=3s --retries=5 CMD ["/roadpass", "health"]
ENTRYPOINT ["/roadpass"]
CMD ["serve"]
