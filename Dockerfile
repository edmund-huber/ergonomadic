FROM golang:alpine

# 6667 == IRC
# 8080 == WebSockets
EXPOSE 6667/tcp 8080/tcp

RUN \
    apk add --update git && \
    rm -rf /var/cache/apk/*

ENTRYPOINT ["/entrypoint"]
CMD ["ergonomadic", "run"]

RUN \
    apk add --update build-base git && \
    rm -rf /var/cache/apk/*

RUN mkdir -p /go/src/ergonomadic
WORKDIR /go/src/ergonomadic

COPY . /go/src/ergonomadic
COPY .dockerfiles/entrypoint.sh /entrypoint

RUN go get -v -d
RUN go install -v
