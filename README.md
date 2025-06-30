# 🧹🪼 JellySweep

> **Intelligent Media Library Management for Jellyfin**  
> Automatically clean up your media collection with smart, data-driven decisions

JellySweep is a sophisticated automation tool that intelligently manages your Jellyfin media library by analyzing viewing patterns, request history, and custom rules to safely remove unwanted content. It integrates seamlessly with your existing *arr stack to make informed cleanup decisions.

---

## ✨ Key Features

- 🤖 **Fully Automated** - Set it and forget it scheduling
- 🧠 **Smart Analytics** - Uses actual viewing data, not just file age
- 🏷️ **Tag-Based Control** - Leverage your existing Sonarr/Radarr tags
- 👥 **User Requests** - Built-in keep request system for community servers
- 🔔 **Smart Notifications** - Email users and ntfy alerts for admins
- ⚡ **Stateless Design** - No database required, clean runs every time
- 🌐 **Web Interface** - Modern UI for monitoring and management

## 🚀 How It Works

JellySweep follows a intelligent multi-stage process:

### 1. **Data Collection**
- Fetches media from Sonarr & Radarr
- Retrieves viewing statistics from Jellystat
- Analyzes request history from Jellyseerr
- Maps media across libraries and services

### 2. **Smart Filtering** 
- Applies configurable age thresholds
- Respects custom exclude tags
- Honors user keep requests

### 3. **Staged Deletion**
- Marks media with dated deletion tags
- Provides grace period for objections
- Removes recently played content from deletion queue
- Executes final cleanup after delay

### 4. **User Interaction**
- Users can request to keep specific media
- Admins can approve/decline requests
- Automatic cleanup of expired requests
- Force deletion override for admins

---

## 🔧 Installation

### Prerequisites
- Access to your Jellyfin ecosystem:
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

## ⚙️ Configuration

JellySweep uses a YAML configuration file with the following structure:

```yaml
jellysweep:
  dry_run: false                    # Set to true for testing
  log_level: "info"                 # debug, info, warn, error
  listen: "0.0.0.0:3002"           # Web interface address and port
  cleanup_interval: 24              # Hours between cleanup runs
  session_key: "your-session-key"  # Random string for session encryption
  
  # Authentication (optional)
  auth:
    oidc:
      enabled: true
      issuer: "https://your-oidc-provider.com/application/o/jellysweep/"
      client_id: "your-client-id"
      client_secret: "your-client-secret"
      redirect_url: "http://localhost:3002/oauth/callback"
      admin_group: "jellyfin-admins"     # OIDC group for admin access
  
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

# Notifications (optional)
jellysweep:
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
```

---

## 🏷️ Tag System

JellySweep uses a the tagging feature from sonarr and radarr to track media state:

### Automatic Tags
- `jellysweep-delete-YYYY-MM-DD` - Media marked for deletion on date
- `jellysweep-keep-request-YYYY-MM-DD` - User requested to keep (expires)
- `jellysweep-must-keep-YYYY-MM-DD` - Admin approved keep (expires)
- `jellysweep-must-delete-for-sure` - Admin forced deletion

### Custom Tags
Configure custom tags in your Sonarr/Radarr to:
- **Exclude from deletion**: Add tags to `exclude_tags` list

---

## 🌐 Web Interface

Access the web interface at `http://localhost:3002` to:

- 📊 **Dashboard**: View deletion queue
- � **Keep Requests**: Submit and manage keep requests
- ⚙️ **Admin Panel**: Approve requests and force deletions

### User Features
- Browse upcoming deletions
- Submit keep requests with reason (TODO)

### Admin Features  
- Review and approve/deny keep requests
- Add permanent keep tags (TODO)

---

## �️ Safety Features

JellySweep includes multiple safety mechanisms:

### Validation Layers
- ✅ **Multi-criteria validation** - All conditions must be met
- ✅ **Grace period** - Configurable delay before actual deletion
- ✅ **Recent play protection** - Recently viewed media is protected
- ✅ **Tag-based exclusions** - Respect custom exclude tags


### Dry Run Mode
- Test your configuration safely
- Preview what would be deleted
- Validate tag assignments
- Debug filtering logic

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

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

---

## � Acknowledgments

- [Jellyfin](https://jellyfin.org/) - The amazing media server
- [Sonarr](https://sonarr.tv/) & [Radarr](https://radarr.video/) - Media management
- [Jellyseerr](https://github.com/Fallenbagel/jellyseerr) - Request management
- [Jellystat](https://github.com/CyferShepard/Jellystat) - Analytics platform

---

<p align="center">
  <strong>⚠️ Always test with dry-run mode first!</strong><br>
  <em>JellySweep is powerful - use responsibly</em>
</p>
