FROM golang:1.23 AS builder
WORKDIR /app
COPY go.mod go.sum* ./
RUN go mod download || true
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/api-tester ./cmd/api-tester

FROM gcr.io/distroless/base-debian12
WORKDIR /app
COPY --from=builder /out/api-tester /app/api-tester
COPY configs /app/configs
EXPOSE 18081
ENTRYPOINT ["/app/api-tester", "-config", "/app/configs/config.yaml"]
