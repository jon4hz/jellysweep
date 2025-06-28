# Jellysweep

Jellysweep is a stateless cleanup service for Jellyfin.

## How does it work?
Jellysweep integrates multiple toolings around Jellyfin to make smart decisions about cleaning media.

- Jellyseerr:
  - Delete media that were requested more than X days ago
- Sonarr:
  - Include or exclude tv shows based on tags
- Radarr:
  - Include or exclude movies based on tags
- Jellystat:
  - Delete media that hasn't been watched for X days

First it fetches media from sonarr and radarr as possible candidates to delete. It then checks jellyseerr and jellystat if the delete conditions match, too. If all conditions are met, the media will be delete using the sonarr / radarr API.

## Why yet another cleaning service?
No other service provided the flexibility and "inteligence" I was looking for so I decided to write my own.
