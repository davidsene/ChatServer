FROM golang:1.8

WORKDIR /go/src/app

COPY chat.go .

RUN go build

EXPOSE 1234

CMD ["./app"]