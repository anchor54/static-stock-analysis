# Stage 1: build the React front end into web/dist.
FROM node:22-slim AS web
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

# Stage 2: build the Go binary with the fresh web/dist embedded.
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/scanner ./cmd/scanner

# Stage 3: small runtime image. CA certificates are needed for HTTPS calls to Yahoo/NSE.
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && adduser -D app
WORKDIR /app
COPY --from=build /out/scanner /app/scanner
RUN mkdir -p /app/data && chown app /app/data
USER app
ENV PORT=8000
EXPOSE 8000
CMD ["/app/scanner", "-data", "/app/data"]
