# syntax=docker/dockerfile:1
FROM golang:1.25-alpine AS build

WORKDIR /app

# Download modules first for better layer caching
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Download tailwindcss cli https://tailwindcss.com/blog/standalone-cli
RUN apk --no-cache add curl libstdc++ libgcc
RUN curl -sLO https://github.com/tailwindlabs/tailwindcss/releases/download/v4.1.11/tailwindcss-linux-x64-musl
RUN chmod +x tailwindcss-linux-x64-musl
RUN mv tailwindcss-linux-x64-musl tailwindcss
RUN ./tailwindcss -i input.css -o public/output.css --minify

# Web server + curation CLI (shared module; both are CGO-free).
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /server .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /gems ./cmd/gems

FROM alpine:3.20

RUN adduser -D -u 10001 appuser

COPY --from=build /server /server
COPY --from=build /gems /gems
COPY --from=build /app/public ./public
COPY --from=build /app/db/migrations ./db/migrations
# Seed data + newsletter scaffolding for the gems CLI (paths are resolved
# relative to the working dir; newsletter issue content itself is snapshotted
# into the DB at send time).
COPY --from=build /app/seed ./seed
COPY --from=build /app/newsletter/templates ./newsletter/templates
COPY --from=build /app/newsletter/issues ./newsletter/issues

# Writable media cache root; mounted as a volume in compose (media_data:/data/media).
ENV MEDIA_DIR=/data/media
RUN mkdir -p /data/media && chown -R appuser /data

USER appuser

EXPOSE 3000

HEALTHCHECK CMD wget -qO- http://localhost:3000/healthz || exit 1

CMD ["/server"]
