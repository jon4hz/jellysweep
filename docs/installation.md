# Installation

## Prerequisites

- Access to your Jellyfin ecosystem including:
  - Sonarr
  - Radarr
  - Jellystat or Streamystats
  - Jellyseerr

## Docker Compose

For a quick deployment using Docker/Podman, create a `compose.yml` file:

```yaml
services:
  jellysweep:
    image: ghcr.io/jon4hz/jellysweep:latest
    container_name: jellysweep
    restart: unless-stopped
    ports:
      - "3002:3002"
    volumes:
      - ./config.yml:/app/config.yml:ro # use config file or env vars
      - ./data:/app/data
      # Mount Jellyfin library paths at the same locations for disk usage monitoring
      # Example: if Jellyfin has /data/movies, mount it the same way here
      # - /data/movies:/data/movies:ro
      # - /data/tv:/data/tv:ro
    environment:
      # You can also override config options with env vars
      - JELLYSWEEP_DRY_RUN=false
      - JELLYSWEEP_LISTEN=0.0.0.0:3002
    # enable debug logs
    # command:
    #   - serve
    #   - --log-level=debug
```

You can either supply the configuration via a `config.yml` file or use environment variables. Your choice!
If you want to use the config file, make sure to create it before starting the container.

```bash
vim ./config.yml  # or use emacs if you're one of those people.
```

Then run:

```bash

# Start the service
docker compose up -d

# View logs
docker compose logs -f jellysweep

# Reset all tags (cleanup command)
docker compose exec jellysweep ./jellysweep reset
```

### Docker Compose with Valkey Cache

You can also configure valkey as caching backend:

```yaml
services:
  jellysweep:
    image: ghcr.io/jon4hz/jellysweep:latest
    container_name: jellysweep
    ports:
      - "3002:3002"
    volumes:
      - ./config.yml:/app/config.yml:ro # create config file before starting the container!
      - ./data:/app/data
    environment:
      # Cache configuration
      - JELLYSWEEP_CACHE_TYPE=redis
      - JELLYSWEEP_CACHE_REDIS_URL=valkey:6379
      # Other configuration
      - JELLYSWEEP_DRY_RUN=false
      - JELLYSWEEP_LISTEN=0.0.0.0:3002
      # ... add you config here or use the config file!
    restart: unless-stopped
    depends_on:
      - valkey
    networks:
      - jellyfin-network

  valkey:
    image: valkey/valkey:8-alpine
    container_name: jellysweep-valkey
    restart: unless-stopped

```