# Immagine di quick-server. Build multi-stage, binario su alpine.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -o /quick-server ./cmd/quick-server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /quick-server /usr/local/bin/quick-server
EXPOSE 8080
ENTRYPOINT ["quick-server"]
