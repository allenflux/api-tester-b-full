FROM golang:1.25 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN mkdir -p /out \
    && go build -o /out/api-tester ./cmd/api-tester \
    && if [ -d ./cmd/export-endpoints ]; then \
         go build -o /out/export-endpoints ./cmd/export-endpoints; \
       else \
         printf '#!/bin/sh\necho "cmd/export-endpoints not found in this project"\nexit 1\n' > /out/export-endpoints \
         && chmod +x /out/export-endpoints; \
       fi

FROM debian:stable-slim

WORKDIR /app

COPY --from=builder /out/api-tester /app/api-tester
COPY --from=builder /out/export-endpoints /app/export-endpoints

COPY configs /app/configs
COPY deploy /app/deploy
COPY docs /app/docs
COPY internal/web/static /app/internal/web/static

RUN mkdir -p /app/data /app/reports

EXPOSE 18081

CMD ["/app/api-tester", "-config", "/app/configs/config.yaml", "-mode", "serve"]