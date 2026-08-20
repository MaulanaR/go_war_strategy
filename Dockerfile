FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/go-war-strategy .

FROM alpine:3.21
RUN adduser -D -H -u 10001 appuser
USER appuser
COPY --from=build /out/go-war-strategy /usr/local/bin/go-war-strategy
ENV PORT=8080
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/go-war-strategy"]
