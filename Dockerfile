FROM golang:1.25-alpine AS build

WORKDIR /app

COPY app/go.mod app/go.sum ./
RUN go mod download

COPY app/ .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/gkfeed-api ./cmd/api


FROM alpine:latest

COPY --from=build /out/gkfeed-api /usr/local/bin/gkfeed-api

ENTRYPOINT ["gkfeed-api"]
