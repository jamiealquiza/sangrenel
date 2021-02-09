## Build image
FROM golang:1.10.3 AS builder

RUN go get -u github.com/jamiealquiza/sangrenel && \
  go build -o /go/bin/sangrenel github.com/jamiealquiza/sangrenel

## Execute image
FROM alpine:3.8

RUN apk add --no-cache libc6-compat
COPY --from=builder /go/bin/sangrenel .

ENTRYPOINT ["/sangrenel"]
CMD ["--help"]
