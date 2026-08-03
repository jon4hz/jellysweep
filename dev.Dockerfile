# Development Dockerfile for Jellysweep
FROM golang:1.26@sha256:3aff6657219a4d9c14e27fb1d8976c49c29fddb70ba835014f477e1c70636647 AS base

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
