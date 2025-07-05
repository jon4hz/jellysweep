#!/bin/bash

# Array to store background process IDs for cleanup
PIDS=()

# Cleanup function to kill all socat processes
cleanup() {
    echo "Cleaning up tunnels..."
    for pid in "${PIDS[@]}"; do
        if kill -0 "$pid" 2>/dev/null; then
            echo "Stopping tunnel (PID: $pid)"
            kill "$pid"
        fi
    done
    echo "All tunnels stopped."
    exit 0
}

# Set up signal handlers for cleanup
trap cleanup SIGINT SIGTERM EXIT

# Function to get first IP address from container (handles multiple networks)
get_container_ip() {
    local container_name="$1"
    local ip
    ip=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{break}}{{end}}' "$container_name" 2>/dev/null)
    echo "$ip"
}

# Function to create tunnel if container exists and has IP
create_tunnel() {
    local container_name="$1"
    local port="$2"
    local ip
    ip=$(get_container_ip "$container_name")

    if [ -z "$ip" ]; then
        echo "Warning: Container '$container_name' not found or has no IP address"
        return 1
    fi

    echo "Creating tunnel for $container_name ($ip:$port -> localhost:$port)"
    socat "TCP-LISTEN:$port,reuseaddr,fork" "TCP:$ip:$port" &
    local pid=$!
    PIDS+=("$pid")

    # Verify the tunnel started successfully
    sleep 0.5
    if ! kill -0 "$pid" 2>/dev/null; then
        echo "Error: Failed to start tunnel for $container_name"
        return 1
    fi

    return 0
}

echo "Setting up Docker container tunnels..."
echo "Press Ctrl+C to stop all tunnels and exit"
echo

# Create tunnels for each service
create_tunnel "jellyseerr" "5055"
create_tunnel "sonarr" "8989"
create_tunnel "radarr" "7878"
create_tunnel "jellystat" "3000"

echo
echo "Active tunnels:"
echo "  jellyseerr:  localhost:5055"
echo "  sonarr:      localhost:8989"
echo "  radarr:      localhost:7878"
echo "  jellystat:   localhost:3000"
echo
echo "Tunnels are running. Press Ctrl+C to stop."

# Wait for all background processes
wait
