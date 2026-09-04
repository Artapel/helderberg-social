# Build a static binary, ship it alone. No shell, no package manager, no
# libc in the final image: nothing for an attacker to run if they get in.
FROM golang:1.24-alpine AS build
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /hs-api .

FROM alpine:3.20 AS certs
RUN apk add --no-cache ca-certificates tzdata

FROM scratch
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=certs /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=build /hs-api /hs-api
USER 65534:65534
EXPOSE 8102
ENTRYPOINT ["/hs-api"]
