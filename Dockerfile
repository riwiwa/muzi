# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# release builds pass the tag, which `muzi -version` reports
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /muzi .

FROM alpine:3.22
# tzdata so TZ sets the server's local time, which days and hours in stats follow
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -H -u 10001 muzi \
    && mkdir -p /data/uploads \
    && chown muzi /data/uploads
COPY --from=build /muzi /usr/local/bin/muzi
USER muzi
WORKDIR /data
ENV MUZI_UPLOADS_DIR=/data/uploads
EXPOSE 1234
VOLUME /data/uploads
# settings come from MUZI_* environment variables, or mount a file and pass -config
ENTRYPOINT ["muzi"]
