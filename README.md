# 🧹🪼 JellySweep

> 🎬 A smart, stateless cleanup service for Jellyfin that automatically manages your media library

JellySweep intelligently removes unwanted media from your Jellyfin server by integrating with multiple services to make informed deletion decisions. Say goodbye to manual cleanup and hello to automated media management! ✨

## 🚀 How does it work?

JellySweep orchestrates multiple tools in the Jellyfin ecosystem to create a comprehensive cleanup strategy:

### 🔍 **Data Sources & Rules**

- **Jellyseerr Integration**
  - 🗓️ Automatically remove media requested more than X days ago

- **Sonarr Integration** 
  - 🔖 Include or exclude TV shows based on custom tags

- **Radarr Integration**
  - 🏷️ Include or exclude movies based on custom tags  

- **Jellystat Integration**
  - ⏰ Remove media that hasn't been watched for X days

### ⚡ **Smart Decision Engine**

1. **🔍 Discovery**: Fetches media from Sonarr and Radarr as deletion candidates
2. **🧠 Analysis**: Cross-references with Jellyseerr request history and Jellystat viewing data  
3. **✅ Validation**: Ensures ALL configured conditions are met before deletion
4. **🗑️ Cleanup**: Safely removes media using the Sonarr/Radarr APIs

## 🎯 Why JellySweep?

No other cleanup service provided the **flexibility** and **intelligence** needed for sophisticated media management. JellySweep fills this gap by:

- 🧩 **Multi-service Integration**: Works seamlessly across your entire *arr stack
- 🎛️ **Granular Control**: Tag-based filtering and multiple threshold options
- 📊 **Data-Driven**: Uses actual viewing statistics, not just file age
- 🔒 **Safe Operations**: Multiple validation layers prevent accidental deletions
- ⚡ **Stateless Design**: No database required, runs clean every time
- 🔄 **Automated**: Set it and forget it - runs on configurable intervals

## 🛠️ Features

- ✨ **Smart Filtering**: Multiple criteria ensure only truly unwanted media is removed
- 🏷️ **Tag Support**: Leverage your existing Sonarr/Radarr tag system
- 📊 **Usage Analytics**: Integrated Jellystat support for viewing-based decisions
- 🔧 **Highly Configurable**: Customizable thresholds and rules for every use case
- 🚀 **Lightweight**: Minimal resource footprint with stateless architecture
- 🔐 **Secure**: Uses official APIs with proper authentication
