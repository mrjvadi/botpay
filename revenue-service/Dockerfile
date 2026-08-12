FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY botpay/go.mod botpay/go.sum ./botpay/
COPY botpay/shared/ botpay/shared/
COPY revenue-service/ revenue-service/
RUN cd revenue-service && go build -o /bin/service ./cmd/...

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /bin/service /service
ENTRYPOINT ["/service"]
