 # JellySweep Production Setup

This directory contains the production Docker Compose configuration for JellySweep.

## Quick Start

1. **Copy the environment template:**
   ```bash
   cp .env.example .env
   ```

2. **Edit the `.env` file with your actual values:**
   - Set your session key (`openssl rand -base64 32`)
   - Configure OIDC authentication details
   - Set your Jellyfin instance URL
   - Add API keys for Jellyseerr, Sonarr, Radarr, and Jellystat
   - Configure notification settings (optional)

3. **Generate VAPID keys for web push notifications:**
   ```bash
   docker run --rm --entrypoint=/usr/local/bin/jellysweep ghcr.io/jon4hz/jellysweep:latest generate-vapid-keys
   ```
   Add the generated keys to your `.env` file.

4. **Start the application:**
   ```bash
   docker compose up -d
   ```

## Configuration

### Required Services
- **Jellyfin**: Media server instance
- **Jellyseerr**: Request management
- **Sonarr**: TV show management
- **Radarr**: Movie management
- **Jellystat**: Analytics and statistics

### Authentication
JellySweep supports two authentication methods in production:

**OIDC Authentication:**
- An OIDC provider (like Authentik, Keycloak, etc.)
- Client ID and secret
- Admin group name for administrative access (reads groups from `group` claim)
- Redirect URL configured in your OIDC provider

**Jellyfin Authentication:**
- Uses your existing Jellyfin user accounts
- Admin access based on Jellyfin administrator privileges


> [!WARNING] 
> You should configure at least one authentication method (OIDC or Jellyfin).


### Library Configuration
The example includes configuration for "TV Shows" and "Movies" libraries with:
- 120-day request age threshold
- 90-day last stream threshold
- 30-day cleanup delay
- Exclusion tags to prevent deletion

Customize these values in the environment variables section.

## Reverse Proxy

JellySweep doesn't handle SSL/TLS termination itself. You'll need to set up a reverse proxy to handle HTTPS and route traffic to the application.

### Traefik Example

```yaml
labels:
  traefik.enable: true
  traefik.http.routers.jellysweep.rule: Host(`jellysweep.yourdomain.com`)
  traefik.http.routers.jellysweep.entrypoints: websecure
  traefik.http.routers.jellysweep.tls: true
  traefik.http.routers.jellysweep.tls.certresolver: letsencrypt
  traefik.http.routers.jellysweep.middlewares: security-headers@file
  traefik.http.services.jellysweep.loadbalancer.server.port: 3002
```

**Security Headers Middleware** (recommended):
```yaml
# traefik/dynamic/middlewares.yml
http:
  middlewares:
    security-headers:
      headers:
        hostsProxyHeaders: X-Forwarded-Host
        customRequestHeaders:
          X-Forwarded-Proto: "https"
        sslRedirect: true
        stsSeconds: 31536000
        stsIncludeSubdomains: true
        stsPreload: true
        forceSTSHeader: true
        customFrameOptionsValue: DENY
        contentTypeNosniff: true
        browserXssFilter: true
        referrerPolicy: strict-origin-when-cross-origin
        customResponseHeaders:
          X-Robots-Tag: none,noarchive,nosnippet,notranslate,noimageindex
```

### Nginx Example

```nginx
# Define upstream for JellySweep
upstream jellysweep_backend {
    server jellysweep:3002;  # Docker service name
    # server 127.0.0.1:3002;  # Alternative for local deployment

    # Health check settings
    keepalive 32;
}

server {
    listen 443 ssl http2;
    listen [::]:443 ssl http2;
    server_name jellysweep.yourdomain.com;

    # SSL Configuration
    ssl_certificate /path/to/your/certificate.pem;
    ssl_certificate_key /path/to/your/private.key;
    ssl_session_timeout 1d;
    ssl_session_cache shared:MozTLS:10m;
    ssl_session_tickets off;

    # Modern SSL configuration
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384;
    ssl_prefer_server_ciphers off;

    # Security Headers
    add_header X-Frame-Options "DENY" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header Referrer-Policy "strict-origin-when-cross-origin" always;
    add_header X-Forwarded-Proto "https" always;

    # HSTS (optional but recommended)
    add_header Strict-Transport-Security "max-age=31536000; includeSubDomains; preload" always;

    # Proxy Configuration
    location / {
        proxy_pass http://jellysweep_backend;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-Host $host;
        proxy_set_header X-Forwarded-Port $server_port;
        
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";

        
        # Timeouts
        proxy_connect_timeout 300s;
        proxy_send_timeout 300s;
        proxy_read_timeout 300s;
    }
}

# Redirect HTTP to HTTPS
server {
    listen 80;
    listen [::]:80;
    server_name jellysweep.yourdomain.com;
    return 301 https://$server_name$request_uri;
}
```

### Important Notes

- **Update OIDC Redirect URL**: Make sure your OIDC provider's redirect URL matches your domain (e.g., `https://jellysweep.yourdomain.com/auth/oidc/callback`)
- **Update Server URL**: Set `JELLYSWEEP_SERVER_URL` to your public domain in the environment variables
- **Network Configuration**: Ensure JellySweep can communicate with your Jellyfin services through the configured networks