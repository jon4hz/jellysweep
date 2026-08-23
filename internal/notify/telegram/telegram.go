package telegram

import (
	"context"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/jon4hz/jellysweep/internal/config"
)

type Config = config.TelegramConfig

func NewClient(cfg *config.TelegramConfig) (*Client, error) {
	telegramBot, err := bot.New(cfg.BotID)
	return &Client{
		bot: telegramBot,
	}, err
}

type Client struct {
	bot *bot.Bot
	cfg *config.TelegramConfig
}

func (c *Client) SendNotification(ctx context.Context, eventType config.EventType, message string) error {
	params := &bot.SendMessageParams{
		ChatID:          c.cfg.ChatID,
		MessageThreadID: c.topicIDForEvent(eventType),
		Text:            message,
		ParseMode:       models.ParseModeMarkdown,
	}
	_, err := c.bot.SendMessage(ctx, params)
	return err
}

func (c *Client) topicIDForEvent(eventType config.EventType) int {
	if topicID, exists := c.cfg.TopicIDsByEvents[eventType]; exists {
		return topicID
	}

	return 0
}
