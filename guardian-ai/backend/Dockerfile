# Build
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
RUN go mod tidy && CGO_ENABLED=0 go build -o /guardian .

# Run
FROM alpine:3.20
RUN adduser -D -u 10001 app
COPY --from=build /guardian /guardian
COPY knowledge/ /knowledge/
ENV KNOWLEDGE_DIR=/knowledge
USER app
EXPOSE 3000
ENTRYPOINT ["/guardian"]
