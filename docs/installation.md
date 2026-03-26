---
title: Installation Guide
description: Guided installation — Prerequistites and Docker Compose
---

# Installation with Docker Compose

!!! abstract "Assumptions"

    This installation guide **assumes the following:**

    - You are comfortable with:
        - Docker Compose
        - Configuration via YAML files (`.yml`)
    - Your other services are also running in Docker

## Prerequisites

Admin access to your Jellyfin media server, and an ecosystem of Jellyfin-related services

=== Recommended Requirements

    - [x] **Jellyfin**
    - [x] \*arr's
        - [x] **Sonarr**
        - [x] **Radarr**
    - [x] A statistics app
        - [x] **Jellystat** ^^or^^ **Streamystats**

=== Minimum requirements

    - [x] **Jellyfin**
    - [x] \*arr's
        - [x] **Sonarr**
        - [x] **Radarr**

=== All supported services

    For a full list, see [How it works: Integrations and features](./how-it-works-integrations.md)

<!-- todo -->

<!-- collapsed block -->

??? question "Can I use less services?"

    Yes. It *is* possible to use Jellysweep with only the following services configured:

    - [x] Jellyfin
    - [x] Sonarr
    - [x] Radarr

    However, this is **not recommended** for the full capabilities of Jellysweep

    This installation guide will asssume you have all of the prerequisites ("Recommended requirements")

## Docker Compose installation

### 1. Docker Compose file

- [ ] Create a Docker Compose file/service
- [ ] Edit the **volume mounts** to match the library mounts **in Jellyfin's library paths**

```yaml title="Docker Compose" hl_lines="14-18"
services:
  jellysweep:
    image: ghcr.io/jon4hz/jellysweep:latest
    container_name: jellysweep
    restart: unless-stopped
    ports:
      - "3002:3002"
    volumes:
      # configuration file
      - ./config.yml:/app/config.yml:ro
      # persistent data
      - ./data:/app/data

      # Jellyfin library
      # • Mount Jellyfin library paths at the same locations for disk usage monitoring
      # • Example: if Jellyfin has /data/movies, mount it the same way here
      # - /data/movies:/data/movies:ro
      # - /data/tv:/data/tv:ro
```

<!-- collapsed block -->

??? example "Valkey Cache backend (advanced)"

    Another configuration is using Valkey Cache as a cache backend

    ```yaml title="Docker Compose" hl_lines="19-27"
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
          # ... add your config here, or use the config file!
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

### 2. Configuration

!!! note "The configuration file must be created prior to starting the Jellysweep container!"

- Configuration is primarily done via a **configuration file**
- Configuration is discussed further in [Configuration](./configuration.md)

<!-- collapsed block -->

??? example "Environmental variables (advanced)"

    Docker **environmental variables** can be used. They **override** options in the configuration file

    ```yaml title="Overriding configuration file with environmental variables" hl_lines="17-20"
    services:
      jellysweep:
        image: ghcr.io/jon4hz/jellysweep:latest
        container_name: jellysweep
        restart: unless-stopped
        ports:
          - "3002:3002"
        volumes:
          # config file
          - ./config.yml:/app/config.yml:ro
          # persistent data
          - ./data:/app/data

          # Jellyfin library
          - /data/movies:/data/movies:ro
          - /data/tv:/data/tv:ro
        environment:
          # Overriding configuration options
          - JELLYSWEEP_DRY_RUN=false
          - JELLYSWEEP_LISTEN=0.0.0.0:3002
    ```

#### 2-1. Create the configuration file

**Create the the configuration file before starting the container!** Located in the container's directory:

```bash title="Jellysweep's Configuration file"
./config.yml
```

#### 2-2. Edit the configuration file

<!-- TODO — all the admonitions + code blocks... this gets messy -->

**Here is a starting template. Use this as `config.yml`:**

!!! note "About the config template"

    - **Optional features** requiring configuration are disabled — set to `false` (or commented out):

        ```yaml
        enabled: false
        ```

    - ==Highlighted== are **enabled** features, which we recommend configuring

=== "Minimal configuration"

    ```yaml linenums="1" hl_lines="1 7 18-21 28-80 82-91"
    dry_run: true                    # Set to true for testing, false for usage
    listen: "0.0.0.0:3002"
    cleanup_schedule: "0 */12 * * *"
    cleanup_mode: "keep_seasons"
    keep_count: 1
    api_key: ""
    session_key: "your-session-key"
    session_max_age: 172800
    secure_cookies: true
    server_url: "http://localhost:3002"

    # Authentication (optional - if no auth is configured, web interface is accessible without authentication)
    auth:
      # Jellyfin Authentication
      jellyfin:
        enabled: true                      # Default authentication method

    # Jellyfin server configuration
    jellyfin:
      url: "http://localhost:8096"         # Your Jellyfin server URL
      api_key: "your-jellyfin-api-key"     # Jellyfin API key

    # Create collections for media scheduled for deletion
    leaving_collections_enabled: false
    leaving_collections_movie_name: "Leaving Movies"
    leaving_collections_tv_name: "Leaving TV Shows"

    # Libraries (and their filters)
    libraries:

      # Name must match the Library name in Jellyfin
      "Movies":
        enabled: true
        cleanup_delay: 60
        protection_period: 90         # Protect requested content for 90 days
        # Filter configuration
        filter:
          content_age_threshold: 120          # Content must be at least 120 days old
          last_stream_threshold: 90           # Last watched at least 90 days ago
          content_size_threshold: 1073741824  # 1GB minimum (0 = no minimum)
          tunarr_enabled: true                # Protect items used by Tunarr channels (requires tunarr config)
          exclude_tags:
            - "jellysweep-exclude"
            - "keep"
            - "favorites"
        # Disk usage-based cleanup for movies
        disk_usage_thresholds:
          - usage_percent: 70.0       # When disk usage reaches 70%
            max_cleanup_delay: 30     # Reduce grace period to 30 days
          - usage_percent: 85.0       # When disk usage reaches 85%
            max_cleanup_delay: 14     # Reduce grace period to 14 days
          - usage_percent: 90.0       # When disk usage reaches 90%
            max_cleanup_delay: 7      # Reduce grace period to 7 days
          - usage_percent: 95.0       # When disk usage reaches 95%
            max_cleanup_delay: 2      # Reduce grace period to 2 days

      "TV Shows":
        enabled: true
        cleanup_delay: 60
        protection_period: 90
        # Filter configuration
        filter:
          content_age_threshold: 120
          last_stream_threshold: 90
          content_size_threshold: 2147483648  # 2GB minimum
          tunarr_enabled: false               # Disable Tunarr filter for this library
          exclude_tags:
            - "jellysweep-exclude"
            - "ongoing"
            - "keep"
        # Disk usage-based cleanup for TV shows
        disk_usage_thresholds:
          - usage_percent: 70.0
            max_cleanup_delay: 30
          - usage_percent: 85.0
            max_cleanup_delay: 14
          - usage_percent: 90.0
            max_cleanup_delay: 7
          - usage_percent: 95.0
            max_cleanup_delay: 2

    # External service integrations
    sonarr:
      url: "http://localhost:8989"
      api_key: "your-sonarr-api-key"
      timeout: 30                          # HTTP client timeout in seconds (default: 30)

    radarr:
      url: "http://localhost:7878"
      api_key: "your-radarr-api-key"
      timeout: 30                          # HTTP client timeout in seconds (default: 30)
    ```

=== "Full configuration"

    ```yaml linenums="1" hl_lines="1 7 33-35 52-55 57-109 142-167"
    dry_run: true                    # Set to true for testing, false for usage
    listen: "0.0.0.0:3002"           # Web interface address and port
    cleanup_schedule: "0 */12 * * *" # Every 12 hours
    cleanup_mode: "keep_seasons"     # Cleanup mode: "all", "keep_episodes", or "keep_seasons"
    keep_count: 1                    # Number of episodes/seasons to keep (when using keep_episodes or keep_seasons)
    api_key: ""                      # Optional: API key for Jellyfin plugin integration
    session_key: "your-session-key"  # Random string for session encryption
    session_max_age: 172800          # Session max age in seconds (48 hours)
    secure_cookies: true             # Set Secure flag on session cookies (disable only for local development)
    # trusted_proxies:               # Optional: list of trusted reverse-proxy IPs/CIDRs
    #   - "10.0.0.1"                 # If unset, all proxies are trusted
    #   - "192.168.1.0/24"
    server_url: "http://localhost:3002"

    # Database configuration (optional)
    database:
      path: "./data/jellysweep.db"

    # Authentication (optional - if no auth is configured, web interface is accessible without authentication)
    auth:
      # OpenID Connect (OIDC) Authentication
      # oidc:
      #   enabled: false
      #   name: OIDC
      #   issuer: "https://login.mydomain.com/application/o/jellysweep/"
      #   client_id: "your-client-id"
      #   client_secret: "your-client-secret"
      #   redirect_url: "http://localhost:3002/auth/oidc/callback"
      #   use_pkce: true                     # Use PKCE for enhanced security
      #   admin_group: "jellyfin-admins"     # OIDC group for admin access
      #   auto_approve_group: "vip-users"    # (Optional) OIDC group for auto-approval of keep requests

      # Jellyfin Authentication
      jellyfin:
        enabled: true                      # Default authentication method

    # Jellyfin server configuration
    jellyfin:
      url: "http://localhost:8096"         # Your Jellyfin server URL
      api_key: "your-jellyfin-api-key"     # Jellyfin API key

    # Profile Pictures (optional)
    gravatar:
      enabled: false                       # Enable Gravatar profile pictures
      default_image: "mp"                  # Default image if no Gravatar found
                                          # Options: "404", "mp", "identicon", "monsterid",
                                          #          "wavatar", "retro", "robohash", "blank"
      rating: "g"                          # Maximum rating for images
                                          # Options: "g", "pg", "r", "x"
      size: 80                             # Image size in pixels (1-2048)

    # Create collections for media scheduled for deletion
    leaving_collections_enabled: true
    leaving_collections_movie_name: "Leaving Movies"
    leaving_collections_tv_name: "Leaving TV Shows"

    # Libraries (and their filters)
    libraries:

      # Name must match the Library name in Jellyfin
      "Movies":
        enabled: true
        cleanup_delay: 60
        protection_period: 90         # Protect requested content for 90 days
        # Filter configuration
        filter:
          content_age_threshold: 120        # Content must be at least 120 days old
          last_stream_threshold: 90         # Last watched at least 90 days ago
          content_size_threshold: 1073741824  # 1GB minimum (0 = no minimum)
          tunarr_enabled: true              # Protect items used by Tunarr channels (requires tunarr config)
          exclude_tags:
            - "jellysweep-exclude"
            - "keep"
            - "favorites"
        # Disk usage-based cleanup for movies
        disk_usage_thresholds:
          - usage_percent: 70.0       # When disk usage reaches 70%
            max_cleanup_delay: 30     # Reduce grace period to 30 days
          - usage_percent: 85.0       # When disk usage reaches 85%
            max_cleanup_delay: 14      # Reduce grace period to 14 days
          - usage_percent: 90.0       # When disk usage reaches 90%
            max_cleanup_delay: 7      # Reduce grace period to 7 days
          - usage_percent: 95.0       # When disk usage reaches 95%
            max_cleanup_delay: 2      # Reduce grace period to 2 days

      "TV Shows":
        enabled: true
        cleanup_delay: 60
        protection_period: 90
        # Filter configuration
        filter:
          content_age_threshold: 120
          last_stream_threshold: 90
          content_size_threshold: 2147483648  # 2GB minimum
          tunarr_enabled: false             # Disable Tunarr filter for this library
          exclude_tags:
            - "jellysweep-exclude"
            - "ongoing"
            - "keep"
        # Disk usage-based cleanup for TV shows
        disk_usage_thresholds:
          - usage_percent: 70.0
            max_cleanup_delay: 30
          - usage_percent: 85.0
            max_cleanup_delay: 14
          - usage_percent: 90.0
            max_cleanup_delay: 7
          - usage_percent: 95.0
            max_cleanup_delay: 2

    # Email notifications for users about upcoming deletions
    email:
      enabled: false
      smtp_host: "mail.example.com"
      smtp_port: 587
      username: "your-smtp-username"
      password: "your-smtp-password"
      from_email: "jellysweep@example.com"
      from_name: "Jellysweep"
      use_tls: true              # Use STARTTLS
      use_ssl: false             # Use SSL/TLS
      insecure_skip_verify: false

    # Ntfy notifications for admins about keep requests and deletions
    ntfy:
      enabled: false
      server_url: "https://ntfy.sh"  # Or your own ntfy server
      topic: "jellysweep"
      # Authentication options (choose one):
      username: ""               # Username/password auth
      password: ""
      token: ""                  # Token auth (takes precedence)

    # Web push notifications
    webpush:
      enabled: false
      vapid_email: "your-email@example.com"  # Contact email for VAPID keys
      public_key: ""                         # VAPID public key
      private_key: ""                        # VAPID private key
      timeout: 30                            # HTTP client timeout in seconds (default: 30)

    # External service integrations
    jellyseerr:
      url: "http://localhost:5055"
      api_key: "your-jellyseerr-api-key"
      timeout: 30                          # HTTP client timeout in seconds (default: 30)

    sonarr:
      url: "http://localhost:8989"
      api_key: "your-sonarr-api-key"
      timeout: 30                          # HTTP client timeout in seconds (default: 30)

    radarr:
      url: "http://localhost:7878"
      api_key: "your-radarr-api-key"
      timeout: 30                          # HTTP client timeout in seconds (default: 30)

    jellystat:
      url: "http://localhost:3001"
      api_key: "your-jellystat-api-key"
      timeout: 30                          # HTTP client timeout in seconds (default: 30)

    # Alternative to Jellystat (configure only one)
    streamystats:
      url: "http://localhost:3001"
      server_id: 1                         # Jellyfin server ID in Streamystats
      timeout: 30                          # HTTP client timeout in seconds (default: 30)

    # Tunarr (optional)
    # Protect items that are used by Tunarr TV channels. When configured, Jellysweep will
    # fetch channel programming and skip deletion for any movie or series that is
    # currently used by a Tunarr program.
    #
    #tunarr:
    #  url: "http://localhost:8000"
    #  timeout: 30                         # HTTP client timeout in seconds (default: 30)

    # Cache configuration (optional - improves performance for large libraries)
    cache:
      enabled: true                  # Enable caching system
      type: "memory"                 # Options: "memory", "redis"
      redis_url: "localhost:6379"    # Redis server URL (when using redis cache)
    ```

#### 2-3. Jellyfin — configuration

<!-- TODO -->

You will need:

- **URL** (as Jellysweep's container can access it)
- **API key**
    - Jellyfin Web dashboard: `Advanced` ➔ `API Keys` ➔ `New API Key`
        - Create a new API key. You can name it `Jellysweep`

```yaml title="config.yml" linenums="37" hl_lines="2-4"
# Jellyfin server configuration
jellyfin:
  url: "http://localhost:8096"         # Your Jellyfin server URL
  api_key: "your-jellyfin-api-key"     # Jellyfin API keyey
```

#### 2-4. Sonarr & Radarr — configuration

You will need:

- **URL** (as Jellysweep's container can access it)
- **API keys**
    - Sonarr + Radarr: `Settings` ➔ `General` ➔ `API Key`

```yaml title="config.yml" linenums="148" hl_lines="1-3 6-8"
sonarr:
  url: "http://localhost:8989"
  api_key: "your-sonarr-api-key"
  timeout: 30                          # HTTP client timeout in seconds (default: 30)

radarr:
  url: "http://localhost:7878"
  api_key: "your-radarr-api-key"
  timeout: 30                          # HTTP client timeout in seconds (default: 30)
```

#### 2-5. Filters — configuration

The template `config.yml` uses 'sane defaults'. But it is likely you will want to adjust the filters

See our [Filters page for more information](./how-it-works-filters.md)

```yaml title="config.yml" linenums="57"
# Libraries (and their filters)
libraries:

  # Name must match the Library name in Jellyfin
  "Movies":
    enabled: true
    cleanup_delay: 60
    protection_period: 90         # Protect requested content for 90 days
    # Filter configuration
    filter:
      content_age_threshold: 120        # Content must be at least 120 days old
      last_stream_threshold: 90         # Last watched at least 90 days ago
      content_size_threshold: 1073741824  # 1GB minimum (0 = no minimum)
      tunarr_enabled: true              # Protect items used by Tunarr channels (requires tunarr config)
      exclude_tags:
        - "jellysweep-exclude"
        - "keep"
        - "favorites"
    # Disk usage-based cleanup for movies
    disk_usage_thresholds:
      - usage_percent: 70.0       # When disk usage reaches 70%
        max_cleanup_delay: 30     # Reduce grace period to 30 days
      - usage_percent: 85.0       # When disk usage reaches 85%
        max_cleanup_delay: 14      # Reduce grace period to 14 days
      - usage_percent: 90.0       # When disk usage reaches 90%
        max_cleanup_delay: 7      # Reduce grace period to 7 days
      - usage_percent: 95.0       # When disk usage reaches 95%
        max_cleanup_delay: 2      # Reduce grace period to 2 days

  "TV Shows":
    enabled: true
    cleanup_delay: 60
    protection_period: 90
    # Filter configuration
    filter:
      content_age_threshold: 120
      last_stream_threshold: 90
      content_size_threshold: 2147483648  # 2GB minimum
      tunarr_enabled: false             # Disable Tunarr filter for this library
      exclude_tags:
        - "jellysweep-exclude"
        - "ongoing"
        - "keep"
    # Disk usage-based cleanup for TV shows
    disk_usage_thresholds:
      - usage_percent: 70.0
        max_cleanup_delay: 30
      - usage_percent: 85.0
        max_cleanup_delay: 14
      - usage_percent: 90.0
        max_cleanup_delay: 7
      - usage_percent: 95.0
        max_cleanup_delay: 2
```

#### 2-8. Other services — configuration

<!-- initially collapsed block -->

??? success "See our configuration page for other configurations"

    See our [configuration page](./configuration.md) for details about other services, configurations:

    - **Jellyseerr/Seerr**
    - **SMTP Emails** to Seerr users
    - **Ntfy** notifications
    - **Tunarr**
    - **OpenID Connect authentication**
    - **Gravatar** profile pictures
    - **Web push notifications**
    - **Backend configuration** (cache, database, etc)

### 3. Start Jellysweep

#### 3-1 Start the Docker container

!!! warning

    #### Reminder:

    Jellysweep can **delete media permanently**

    - Take backups of critical data
    - Write your configuration carefully
    - Disable `dry_run` **with caution**

```bash title="Start the container"
docker compose up -d
```

```bash title="View Jellysweep's logs"
docker compose logs -f jellysweep
```

Jellysweep should start, and should be available at `http://ip-address:3001`! :tada:

[Any issues? Check our troubleshooting pages](./troubleshooting.md)

#### 3-2. Disabling dry-run

With caution, dry-run mode can be disabled. This means Jellysweep **will delete media**, according to your filters

```yaml title="config.yml" linenums="1"
dry_run: true                    # Set to true for testing, false for usage
```
