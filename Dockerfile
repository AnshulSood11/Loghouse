FROM golang:1.22 AS build
WORKDIR /go/src/loghouse
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /go/bin/loghouse ./cmd/
RUN CGO_ENABLED=0 GOOS=linux go build -o /go/bin/health_check ./cmd/health_check/
RUN chmod +x /go/bin/loghouse
RUN chmod +x /go/bin/health_check
EXPOSE 8400
ENTRYPOINT ["/go/bin/loghouse"]