FROM golang:1.25-alpine AS builder

ENV GO111MODULE=auto \
    CGO_ENABLED=0 \
    LD_FLAGS="-w -s"

WORKDIR /build
RUN apk add --no-cache --update git

COPY ./ .
RUN set -ex \
    && echo "运行 go generate..." \
    && go generate ./...

RUN set -ex \
    && echo "开始构建..." \
    && go build -trimpath -ldflags "$LD_FLAGS -extldflags '-static'" -o cqhttp .

FROM alpine:latest

COPY docker-entrypoint.sh /docker-entrypoint.sh

RUN chmod +x /docker-entrypoint.sh && \
    apk add --no-cache --update \
      ffmpeg \
      coreutils \
      shadow \
      su-exec \
      tzdata && \
    rm -rf /var/cache/apk/* && \
    mkdir -p /app && \
    mkdir -p /data && \
    mkdir -p /config && \
    useradd -d /config -s /bin/sh abc && \
    chown -R abc /config && \
    chown -R abc /data

ENV TZ="Asia/Shanghai"
ENV UID=99
ENV GID=100
ENV UMASK=002

COPY --from=builder /build/cqhttp /app/

WORKDIR /data

VOLUME [ "/data" ]

ENTRYPOINT [ "/docker-entrypoint.sh" ]
CMD [ "/app/cqhttp" ]
