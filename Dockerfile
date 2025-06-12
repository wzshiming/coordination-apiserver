FROM docker.io/library/golang:1.24.3-alpine AS builder

WORKDIR /workspace
COPY . .

RUN CGO_ENABLED=0 go build -o coordination-apiserver ./cmd/coordination-apiserver

FROM docker.io/library/alpine:3.22

WORKDIR /
COPY --from=builder /workspace/coordination-apiserver /coordination-apiserver

ENTRYPOINT ["/coordination-apiserver"]
