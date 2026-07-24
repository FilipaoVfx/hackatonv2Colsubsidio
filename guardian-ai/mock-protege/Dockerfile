# Build
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
RUN CGO_ENABLED=0 go build -o /protegemock .

# Run
FROM alpine:3.20
RUN adduser -D -u 10001 app
COPY --from=build /protegemock /protegemock
USER app
EXPOSE 9000
ENTRYPOINT ["/protegemock"]
