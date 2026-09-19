FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

ARG TARGETARCH

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOARCH=$TARGETARCH go build -trimpath -o ebookfs .

FROM alpine:3.21

RUN apk add --no-cache ca-certificates && \
    adduser -D -u 1000 ebookfs && \
    mkdir -p /var/lib/ebookfs/library /etc/ebookfs && \
    chown -R ebookfs:ebookfs /var/lib/ebookfs

WORKDIR /app
COPY --from=builder /app/ebookfs .

USER ebookfs

EXPOSE 5640

ENTRYPOINT ["/app/ebookfs"]
