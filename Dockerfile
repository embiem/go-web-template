FROM golang:1.23-alpine as golang

WORKDIR /app
COPY . .

RUN go mod download
RUN go mod verify

# Download tailwindcss cli https://tailwindcss.com/blog/standalone-cli
RUN apk --no-cache add curl
RUN curl -sLO https://github.com/tailwindlabs/tailwindcss/releases/download/v3.4.10/tailwindcss-linux-x64
RUN chmod +x tailwindcss-linux-x64
RUN mv tailwindcss-linux-x64 tailwindcss
RUN ./tailwindcss -i input.css -o public/output.css --minify

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /server .

FROM alpine

COPY --from=golang /server .
COPY --from=golang /app/public ./public
COPY --from=golang /app/db/migrations ./db/migrations

EXPOSE 3000

CMD ["/server"]