# Build Stage
FROM golang:1.24-alpine AS builder  

WORKDIR /app
COPY . .
RUN go mod download

# Build the binary with optimizations
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o app

# Final Image (using alpine to include CA certs)
FROM alpine:3.18
WORKDIR /root/

# Copy only the built binary
COPY --from=builder /app/app .

# Run the application
ENTRYPOINT ["/root/app"]
