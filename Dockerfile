FROM golang:1.6-alpine

RUN apk --update add git \
    && rm /var/cache/apk/*

COPY sangrenel.go /go/src/sangrenel/sangrenel.go
COPY graphite.go /go/src/sangrenel/graphite.go

WORKDIR /go/src/sangrenel
RUN go get
RUN go install sangrenel

ENTRYPOINT ["sangrenel"]

