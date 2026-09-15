# Development Dockerfile for Jellysweep
FROM golang:1.27@sha256:f44f6e88636cfb311f9ebace870ded69d943f227bb3cb27d32ffd84ea18c43ea AS base

# Install Node.js 25 (from .nvmrc)
RUN curl -fsSL https://deb.nodesource.com/setup_25.x | bash - \
    && apt-get install -y nodejs

# Set working directory
WORKDIR /app

# Copy package.json for npm dependencies
COPY package.json ./

# Install npm dependencies
RUN npm install

# Copy go mod files first for better caching
COPY go.mod go.sum ./

# Download Go dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN make build

RUN go build -o jellysweep .

# Expose the default port (adjust if needed)
EXPOSE 3002

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["./jellysweep", "healthcheck"]

# Run the application
CMD ["./jellysweep", "serve", "--log-level", "debug"]
