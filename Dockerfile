FROM golang:1.22-alpine as builder

RUN mkdir -p /build
WORKDIR /build

COPY Makefile go.mod go.sum ./
COPY ./cmd ./cmd/
COPY ./internal ./internal/

# hadolint ignore=DL3018
RUN apk add --no-cache gcc musl-dev make && make

FROM alpine:3

EXPOSE 9000

# hadolint ignore=DL3018
RUN \
  apk add --no-cache bash curl && \
  adduser chaosmonkey -D -h /home/chaosmonkey -s /bin/bash -u 1999 chaosmonkey

COPY --from=builder /build/bin/chaos-monkey /usr/bin/chaos-monkey

WORKDIR /home/chaosmonkey
USER chaosmonkey

CMD ["chaos-monkey"]
