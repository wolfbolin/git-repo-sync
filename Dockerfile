FROM hub.wiolfi.net:23333/docker.io/golang:1.25-alpine AS builder
WORKDIR /opt/builder
ENV GOPROXY=https://goproxy.cn,direct
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o git-repo-sync ./cmd/git-repo-sync
RUN CGO_ENABLED=0 go build -o git-repo-sync-daemon ./cmd/git-repo-sync-daemon

FROM hub.wiolfi.net:23333/docker.io/alpine:latest
RUN apk add --no-cache ca-certificates
COPY --from=builder /opt/builder/git-repo-sync /usr/local/bin/
COPY --from=builder /opt/builder/git-repo-sync-daemon /usr/local/bin/
WORKDIR /usr/local/bin/
CMD ["git-repo-sync-daemon", "-c", "/opt/git-repo-sync/config.yaml"]
