FROM golang:1.25-alpine AS builder
ENV GOTOOLCHAIN=auto

WORKDIR /app

# Install git and ca-certificates for go mod download
RUN apk add --no-cache git ca-certificates

# Copy module file and download deps
COPY go.mod ./
RUN go mod download

# Copy source code
COPY . .

# Ensure module graph is complete and go.sum is populated
RUN go mod tidy

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o main ./cmd/push

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /root/

# Copy the binary from builder stage
COPY --from=builder /app/main .

# Expose port
EXPOSE 8090

# Run the binary
CMD ["./main"]