FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o /bin/service ./cmd/...

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /bin/service /service
ENTRYPOINT ["/service"]
