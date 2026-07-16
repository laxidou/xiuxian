FROM golang:1.24-alpine AS build
ARG BINARY=game-server
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/app ./cmd/${BINARY}

FROM alpine:3.22
RUN addgroup -S app && adduser -S -G app app
WORKDIR /app
COPY --from=build /out/app /usr/local/bin/app
COPY migrations ./migrations
USER app
ENTRYPOINT ["/usr/local/bin/app"]
