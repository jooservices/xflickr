FROM golang:1.26-alpine AS go-build
WORKDIR /src
COPY backend/go.mod backend/go.sum ./backend/
WORKDIR /src/backend
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/xflickr ./cmd/xflickr

FROM node:24-alpine AS web-build
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM node:24-alpine AS runtime
ENV HTTP_PORT=8080 \
    XFLICKR_WEB_DIR=/app/web \
    BLOB_LOCAL_DIR=/app/data
WORKDIR /app
COPY --from=go-build --chown=node:node /out/xflickr /app/xflickr
COPY --from=web-build --chown=node:node /src/web/dist /app/web
RUN mkdir -p /app/data && chown node:node /app/data
USER node
EXPOSE 8080
CMD ["/app/xflickr"]
