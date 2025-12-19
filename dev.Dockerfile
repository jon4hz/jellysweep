# Development Dockerfile for Jellysweep
FROM golang:1.25 AS base

# Install Node.js 22 (from .nvmrc)
RUN curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
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

# Run the application
CMD ["./jellysweep", "serve", "--log-level", "debug"]
