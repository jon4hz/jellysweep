# 🧹🪼 JellySweep

<img src="static/jellysweep.png" alt="JellySweep Logo" width="40%" height="20%">

JellySweep is a smart cleanup tool for your Jellyfin media server.  
It automatically removes old, unwatched movies and TV shows by analyzing your viewing history and user requests.

> [!CAUTION]  
> Always test with dry-run mode first!  
> JellySweep is powerful - configure it correctly!


---

## ✨ Key Features

- 🧠 **Smart Analytics** - Checks jellyseerr for requests and Jellystat of stats
- 🏷️ **Tag-Based Control** - Leverage your existing Sonarr/Radarr tags to control jellysweep
- 👥 **User Requests** - Built-in keep request system for your users
- 🔔 **Notifications** - Email users and ntfy alerts for admins
- ⚡ **Stateless Design** - No database required, clean runs every time
- 🌐 **Web Interface** - Modern UI for monitoring and management


## 📋 Table of Contents

- [🧹🪼 JellySweep](#-jellysweep)
  - [✨ Key Features](#-key-features)
  - [📋 Table of Contents](#-table-of-contents)
  - [🚀 How It Works](#-how-it-works)
  - [🔧 Installation](#-installation)
    - [Prerequisites](#prerequisites)
    - [Quick Start](#quick-start)
  - [🔐 Authentication](#-authentication)
    - [OIDC/SSO Authentication](#oidcsso-authentication)
    - [Jellyfin Authentication](#jellyfin-authentication)
  - [⚙️ Configuration](#️-configuration)
  - [🏷️ Tag System](#️-tag-system)
    - [Automatic Tags](#automatic-tags)
    - [Custom Tags](#custom-tags)
  - [🔧 Commands](#-commands)
  - [🤝 Contributing](#-contributing)
    - [Development Setup](#development-setup)
  - [📄 License](#-license)

## 🚀 How It Works
1. **Data Collection**
   - Fetches media from Sonarr & Radarr
   - Retrieves viewing statistics from Jellystat
   - Analyzes request history from Jellyseerr
   - Maps media across libraries and services

2. **Media Filtering**
   - Applies configurable age thresholds
   - Respects custom exclude tags
   - Respects user keep requests

3. **Delayed Deletion**
   - Marks media with dated deletion tags
   - Provides grace period for objections
   - Removes recently played content from deletion queue
   - Executes final cleanup after delay

4. **User Interaction**
   - Users can request to keep specific media
   - Admins can approve/decline requests
   - Automatic cleanup of expired requests
   - Force deletion override for admins

---

## 🔧 Installation

### Prerequisites
- Access to your Jellyfin ecosystem including:
  - Sonarr
  - Radarr
  - Jellystat
  - Jellyseerr

### Quick Start

1. **Download & Build**
   ```bash
   git clone https://github.com/yourusername/jellysweep.git
   cd jellysweep
   go build -o jellysweep .
   ```

2. **Configuration**
   ```bash
   cp config.example.yml config.yml
   # Edit config.yml with your service URLs and API keys
   ```

3. **Run**
   ```bash
   # Start the service
   ./jellysweep
   
   # Reset all tags (cleanup command)
   ./jellysweep reset
   ```

---

## 🔐 Authentication

JellySweep supports multiple authentication methods to secure your web interface:

### OIDC/SSO Authentication

- **Tested Providers**: Authentik
- **Group-based Admin Access**: Read admin groups from `group` claim.
- **Single Sign-On**: Users authenticate once and access JellySweep seamlessly

**Configuration:**
```yaml
jellysweep:
  auth:
    oidc:
      enabled: true
      issuer: "https://your-sso-provider.com/application/o/jellysweep/"
      client_id: "your-client-id"
      client_secret: "your-client-secret"
      redirect_url: "http://localhost:3002/auth/oidc/callback"
      admin_group: "jellyfin-admins"  # Users in this group get admin access
```

### Jellyfin Authentication

- **Direct Integration**: Leverages your existing Jellyfin user database
- **Admin Detection**: Jellyfin administrators automatically get admin access in JellySweep
- **No Additional Setup**: Works out of the box with your Jellyfin instance
- **Form-based Login**: Traditional username/password login form

**Configuration:**
```yaml
jellysweep:
  auth:
    jellyfin:
      enabled: true
      url: "http://localhost:8096"  # Your Jellyfin server URL
```

---

## ⚙️ Configuration

JellySweep uses a YAML configuration file with the following structure:

```yaml
jellysweep:
  dry_run: false                   # Set to true for testing
  log_level: "info"                # debug, info, warn, error
  listen: "0.0.0.0:3002"           # Web interface address and port
  cleanup_interval: 12             # Hours between cleanup runs
  session_key: "your-session-key"  # Random string for session encryption
  
  # Authentication (optional - choose one or both)
  auth:
    # OpenID Connect (OIDC) Authentication
    oidc:
      enabled: true
      issuer: "https://your-oidc-provider.com/application/o/jellysweep/"
      client_id: "your-client-id"
      client_secret: "your-client-secret"
      redirect_url: "http://localhost:3002/auth/oidc/callback"
      admin_group: "jellyfin-admins"     # OIDC group for admin access
    
    # Jellyfin Authentication
    # Use existing Jellyfin user accounts for authentication
    jellyfin:
      enabled: false                     # Set to true to enable
      url: "http://localhost:8096"       # Your Jellyfin server URL
  
  # Library-specific settings
  libraries:
    "Movies":
      enabled: true
      request_age_threshold: 30     # Days since Jellyseerr request
      last_stream_threshold: 60     # Days since last viewed
      cleanup_delay: 7              # Grace period before deletion
      exclude_tags:
        - "jellysweep-exclude"
        - "keep"
        - "favorites"
    
    "TV Shows":
      enabled: true
      request_age_threshold: 45
      last_stream_threshold: 90
      cleanup_delay: 14
      exclude_tags:
        - "jellysweep-exclude"
        - "ongoing"
        - "keep"
    
  # Email notifications for users about upcoming deletions
  email:
    enabled: true
    smtp_host: "mail.example.com"
    smtp_port: 587
    username: "your-smtp-username"
    password: "your-smtp-password"
    from_email: "jellysweep@example.com"
    from_name: "JellySweep"
    use_tls: true              # Use STARTTLS
    use_ssl: false             # Use implicit SSL/TLS

  # Ntfy notifications for admins about keep requests and deletions
  ntfy:
    enabled: true
    server_url: "https://ntfy.sh"  # Or your own ntfy server
    topic: "jellysweep"
    # Authentication options (choose one):
    username: ""               # Username/password auth
    password: ""
    token: ""                  # Token auth (takes precedence)

# Service integrations (all optional)
jellyseerr:
  url: "http://localhost:5055"
  api_key: "your-jellyseerr-api-key"

sonarr:
  url: "http://localhost:8989"
  api_key: "your-sonarr-api-key"

radarr:
  url: "http://localhost:7878"
  api_key: "your-radarr-api-key"

jellystat:
  url: "http://localhost:3001"
  api_key: "your-jellystat-api-key"
```

---

## 🏷️ Tag System

JellySweep uses the tagging feature from sonarr and radarr to track media state:

### Automatic Tags
- `jellysweep-delete-YYYY-MM-DD` - Media marked for deletion on date
- `jellysweep-keep-request-YYYY-MM-DD` - User requested to keep (expires)
- `jellysweep-must-keep-YYYY-MM-DD` - Admin approved keep (expires)
- `jellysweep-must-delete-for-sure` - Admin forced deletion

### Custom Tags
Configure custom tags in your Sonarr/Radarr to:
- **Exclude from deletion**: Add tags to `exclude_tags` list

---

## 🔧 Commands

```bash
# Start the main service
./jellysweep

# Reset all JellySweep tags (cleanup)
./jellysweep reset

# Validate configuration
./jellysweep --dry-run

# Enable debug logging
./jellysweep --log-level debug
```

---

## 🤝 Contributing

Contributions of all kinds are welcome!

### Development Setup
```bash
git clone https://github.com/yourusername/jellysweep.git
cd jellysweep
go mod download
go run . --dry-run
```

---

## 📄 License

This project is licensed under the GPLv3 License - see the [LICENSE](LICENSE) file for details.
