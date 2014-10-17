FROM ubuntu:14.04

MAINTAINER ???

RUN apt-get update; apt-get install -y git mercurial golang

ENV GOPATH /home/gocode
ADD sangrenel.go /home/gocode/src/github.com/sangrenel/
WORKDIR /home/gocode/src/github.com/sangrenel
RUN go get
RUN go install

CMD ["-h"]
ENTRYPOINT ["/home/gocode/bin/sangrenel"]
