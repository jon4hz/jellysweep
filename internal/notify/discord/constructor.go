package discord

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Discord API limits.
const (
	maxFieldValue   = 1024
	maxEmbedChars   = 6000
	maxFields       = 25
	maxEmbedsPerMsg = 10
)

// discordImageEmbed represents an image or thumbnail in a Discord embed.
type discordImageEmbed struct {
	URL      string `json:"url,omitempty"`
	ProxyURL string `json:"proxy_url,omitempty"`
	Height   int    `json:"height,omitempty"`
	Width    int    `json:"width,omitempty"`
}

// discordField represents a single field in a Discord rich embed.
type discordField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

// discordRichEmbed is the full Discord rich embed object sent via a webhook.
type discordRichEmbed struct {
	Title       string             `json:"title,omitempty"`
	Type        string             `json:"type,omitempty"` // always "rich" for webhooks
	Description string             `json:"description,omitempty"`
	URL         string             `json:"url,omitempty"`
	Timestamp   string             `json:"timestamp,omitempty"`
	Color       int                `json:"color,omitempty"`
	Footer      *discordFooter     `json:"footer,omitempty"`
	Image       *discordImageEmbed `json:"image,omitempty"`
	Thumbnail   *discordImageEmbed `json:"thumbnail,omitempty"`
	Author      *discordAuthor     `json:"author,omitempty"`
	Fields      []discordField     `json:"fields,omitempty"`
}

// discordFooter represents the footer of a Discord embed.
type discordFooter struct {
	Text         string `json:"text"`
	IconURL      string `json:"icon_url,omitempty"`
	ProxyIconURL string `json:"proxy_icon_url,omitempty"`
}

// discordAuthor represents the author section of a Discord embed.
type discordAuthor struct {
	Name         string `json:"name,omitempty"`
	URL          string `json:"url,omitempty"`
	IconURL      string `json:"icon_url,omitempty"`
	ProxyIconURL string `json:"proxy_icon_url,omitempty"`
}

// discordWebhookPayload is the top-level payload posted to a Discord webhook URL.
type discordWebhookPayload struct {
	Embeds          []discordRichEmbed      `json:"embeds"`
	Username        string                  `json:"username,omitempty"`
	AvatarURL       string                  `json:"avatar_url,omitempty"`
	TTS             bool                    `json:"tts"`
	Content         string                  `json:"content,omitempty"`
	AllowedMentions *discordAllowedMentions `json:"allowed_mentions,omitempty"`
}

// discordAllowedMentions controls which mentions are processed in a Discord message.
type discordAllowedMentions struct {
	Parse []string `json:"parse,omitempty"`
	Roles []string `json:"roles,omitempty"`
	Users []string `json:"users,omitempty"`
}

// BuildWebhookPayloads constructs one or more Discord webhook payloads for a cleanup
// notification, splitting across multiple messages when Discord's size limits are hit.
// Items with DiscordEnabled and a non-empty DiscordID will be mentioned by their Discord
// user ID; otherwise the requester's display name is used.
func BuildWebhookPayloads(items []MediaItem, jellysweepURL, username, avatarURL string) []discordWebhookPayload {
	if username == "" {
		username = "Jellysweep"
	}
	baseURL := strings.TrimRight(jellysweepURL, "/")
	if avatarURL == "" && baseURL != "" {
		avatarURL = baseURL + "/static/jellysweep.png"
	}

	summary := buildSummary(items)
	fields := buildFields(items, jellysweepURL)
	embeds := packEmbeds(fields, jellysweepURL, summary)

	var payloads []discordWebhookPayload
	for i := 0; i < len(embeds); i += maxEmbedsPerMsg {
		end := i + maxEmbedsPerMsg
		if end > len(embeds) {
			end = len(embeds)
		}
		payloads = append(payloads, discordWebhookPayload{
			Username:  username,
			AvatarURL: avatarURL,
			TTS:       false,
			Embeds:    embeds[i:end],
		})
	}
	return payloads
}

// buildFields converts media items into Discord fields, splitting date groups when their
// content would exceed Discord's per-field character limit.
func buildFields(items []MediaItem, jellysweepURL string) []discordField {
	// Sort items by deletion date (zero times go last).
	sorted := make([]MediaItem, len(items))
	copy(sorted, items)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].CleanupDate.IsZero() {
			return false
		}
		if sorted[j].CleanupDate.IsZero() {
			return true
		}
		return sorted[i].CleanupDate.Before(sorted[j].CleanupDate)
	})

	// Group items by calendar day (UTC).
	type dateGroup struct {
		day   time.Time
		items []MediaItem
	}
	var groups []dateGroup
	for _, item := range sorted {
		day := item.CleanupDate.UTC().Truncate(24 * time.Hour)
		if len(groups) == 0 || !groups[len(groups)-1].day.Equal(day) {
			groups = append(groups, dateGroup{day: day})
		}
		groups[len(groups)-1].items = append(groups[len(groups)-1].items, item)
	}

	var fields []discordField
	for _, g := range groups {
		dateLabel := "Unknown date"
		if !g.day.IsZero() {
			dateLabel = g.day.Format("January 2, 2006")
		}

		// Build item lines and pack them into fields, respecting maxFieldValue.
		var currentLines []string
		currentLen := 0
		partIndex := 0

		flushField := func() {
			if len(currentLines) == 0 {
				return
			}
			name := dateLabel
			if partIndex > 0 {
				name = dateLabel + " (cont.)"
			}
			fields = append(fields, discordField{
				Name:  name,
				Value: strings.Join(currentLines, "\n"),
			})
			partIndex++
			currentLines = nil
			currentLen = 0
		}

		for _, item := range g.items {
			line := mediaTypeEmoji(item.MediaType) + " **" + item.Title + "**"
			if requester := formatRequester(item); requester != "" {
				line += " · " + requester
			}

			// +1 for the newline separator between lines
			needed := len(line)
			if currentLen > 0 {
				needed++ // newline
			}

			if currentLen+needed > maxFieldValue {
				flushField()
			}

			if currentLen > 0 {
				currentLen++ // newline
			}
			currentLines = append(currentLines, line)
			currentLen += len(line)
		}
		flushField()
	}

	if jellysweepURL != "" {
		fields = append(fields, discordField{
			Name:  "\u200b",
			Value: fmt.Sprintf("🔔 Want to keep something? Request to keep it before the deletion date in [Jellysweep](%s).", jellysweepURL),
		})
	}

	return fields
}

// buildSummary returns a short summary string counting items by media type.
func buildSummary(items []MediaItem) string {
	tvCount, movieCount := 0, 0
	for _, item := range items {
		if item.MediaType == "movie" {
			movieCount++
		} else {
			tvCount++
		}
	}
	parts := []string{fmt.Sprintf("🗑️ Total: %d", len(items))}
	if tvCount > 0 {
		parts = append(parts, fmt.Sprintf("📺 TV Shows: %d", tvCount))
	}
	if movieCount > 0 {
		parts = append(parts, fmt.Sprintf("🎬 Movies: %d", movieCount))
	}
	return strings.Join(parts, "\n")
}

// packEmbeds distributes fields across embeds, respecting the per-embed field count and
// total character limits.
func packEmbeds(fields []discordField, jellysweepURL, summary string) []discordRichEmbed {
	title := "Media Being Deleted Soon"
	// Fixed overhead per embed: title + author name + timestamp (rough estimate).
	const fixedOverhead = 60

	var embeds []discordRichEmbed

	newEmbed := func() discordRichEmbed {
		return discordRichEmbed{
			Type:      "rich",
			Title:     title,
			Color:     0xE67E22,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Author: &discordAuthor{
				Name: "Jellysweep",
				URL:  jellysweepURL,
			},
		}
	}

	current := newEmbed()
	currentChars := fixedOverhead + len(title)

	for _, f := range fields {
		fieldChars := len(f.Name) + len(f.Value)
		if len(current.Fields) >= maxFields || currentChars+fieldChars > maxEmbedChars {
			embeds = append(embeds, current)
			current = newEmbed()
			currentChars = fixedOverhead + len(title)
		}
		current.Fields = append(current.Fields, f)
		currentChars += fieldChars
	}

	if len(current.Fields) > 0 {
		embeds = append(embeds, current)
	}

	if len(embeds) > 0 && summary != "" {
		embeds[0].Description = "📊 **Summary:**\n" + summary
	}

	return embeds
}

// mediaTypeEmoji returns an emoji + label for a media type string.
func mediaTypeEmoji(mediaType string) string {
	switch mediaType {
	case "tv", "show":
		return "📺"
	case "movie":
		return "🎬"
	default:
		return "📁"
	}
}

// formatRequester returns a Discord user mention if the item's requester has Discord enabled,
// otherwise returns the plain display name.
func formatRequester(item MediaItem) string {
	if item.DiscordID != "" {
		return fmt.Sprintf("<@%s>", item.DiscordID)
	}
	return item.RequestedBy
}
