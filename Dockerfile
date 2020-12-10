FROM golang:alpine as builder

COPY . /go/src/github.com/Luzifer/twitch-manager
WORKDIR /go/src/github.com/Luzifer/twitch-manager

RUN set -ex \
 && apk add --update git \
 && go install \
      -ldflags "-X main.version=$(git describe --tags --always || echo dev)" \
      -mod=readonly

FROM alpine:latest

LABEL maintainer "Knut Ahlers <knut@ahlers.me>"

ENV ASSET_DIR=/data/public \
    STORE_FILE=/data/store.json.gz

RUN set -ex \
 && apk --no-cache add \
      ca-certificates

COPY --from=builder /go/bin/twitch-manager /usr/local/bin/twitch-manager

EXPOSE 3000
VOLUME ["/data"]

ENTRYPOINT ["/usr/local/bin/twitch-manager"]
CMD ["--"]

# vim: set ft=Dockerfile:
