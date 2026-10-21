# QuantumCash node. Experimental testnet, not money.
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /qcash ./cmd/qcash

FROM alpine:3.20
RUN adduser -D -h /data qcash
COPY --from=build /qcash /usr/local/bin/qcash
USER qcash
WORKDIR /data
VOLUME /data
EXPOSE 7101 8645
ENTRYPOINT ["qcash", "node", "-network", "testnet"]
CMD ["-data", "/data/testnet-chain.json"]
