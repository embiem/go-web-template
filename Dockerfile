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

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /server .

FROM alpine:3.20

RUN adduser -D -u 10001 appuser

COPY --from=build /server /server
COPY --from=build /app/public ./public
COPY --from=build /app/db/migrations ./db/migrations

USER appuser

EXPOSE 3000

HEALTHCHECK CMD wget -qO- http://localhost:3000/healthz || exit 1

CMD ["/server"]
