# Copyright The OpenTelemetry
FROM golang:1.20 as build
WORKDIR /app/
COPY . .
RUN go env -w GOPROXY=direct
RUN CGO_ENABLED=0 go build -o main ./cmd/main.go
FROM alpine:3.18
COPY --from=build /app/main  /app/main
EXPOSE 8080
CMD ["/app/main"]
