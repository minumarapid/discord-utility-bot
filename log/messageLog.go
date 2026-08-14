package msglog

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	dgr "github.com/minumarapid/discord-go-router"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type LogChannelConfig uint8

// tableに存在したらdisable
type msgLogChannelSetting struct {
	ChannelID string `gorm:"primaryKey"`
	GuildID   string `gorm:"index"`
}

type messageLogChannelRegistry struct {
	DisableChannel sync.Map // key: channelID string, value: guildID string ヒットしたらdisable
	GuildConfig    sync.Map // key: guildID string, value: messageLogGuildConfig
	MsgLogCache    sync.Map // key: messageID string, value: msgLogCache
}

type LogGuildConfig uint8

const (
	GlConfigNone   LogGuildConfig = 0
	GlConfigDelete LogGuildConfig = 1 << (iota - 1)
	GlConfigEdit
)

func messageLogLoadDisabledChannels(db *gorm.DB) (*messageLogChannelRegistry, error) {
	var chConfRows []msgLogChannelSetting
	err := db.Model(&msgLogChannelSetting{}).Find(&chConfRows).Error
	if err != nil {
		return nil, err
	}
	var glConfRows []msgLogGuildSetting
	err = db.Model(&msgLogGuildSetting{}).Find(&glConfRows).Error
	if err != nil {
		return nil, err
	}

	registry := &messageLogChannelRegistry{}

	for _, row := range chConfRows {
		registry.DisableChannel.Store(row.ChannelID, row.GuildID)
	}

	for _, row := range glConfRows {
		registry.GuildConfig.Store(row.GuildID, msgLogGuildSettingConfig{
			LogChannel: row.LogChannel,
			Config:     row.Config,
		})
	}

	return registry, nil
}

type msgLogGuildSettingConfig struct {
	LogChannel string
	Config     LogGuildConfig
}

// tableに存在したらenable
type msgLogGuildSetting struct {
	GuildID    string `gorm:"primaryKey"`
	LogChannel string
	Config     LogGuildConfig
}

type msgLogCache struct {
	MessageID string `gorm:"primaryKey"`
	Content   string
	AuthorID  string
	ChannelID string
	CreatedAt time.Time `gorm:"index"`
}

func isAdmin[T any](c *dgr.Context[T]) bool {
	perms := c.Interaction.Member.Permissions
	return perms&discordgo.PermissionManageGuild != 0
}

func MessageLog(bot *dgr.Dgr, db *gorm.DB) {
	db.AutoMigrate(
		&msgLogChannelSetting{},
		&msgLogCache{},
		&msgLogGuildSetting{},
	)

	go func() {
		cleanup := func() {
			retentionPeriod := time.Now().AddDate(0, 0, -30)
			result := db.
				Where("created_at < ?", retentionPeriod).
				Delete(&msgLogCache{})

			if result.Error != nil {
				log.Println("Failed to clean up old message cache:", result.Error)
			} else {
				log.Printf("Successfully cleaned up %d old message cache record(s)", result.RowsAffected)
			}
		}
		cleanup()
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()

		for range ticker.C {
			cleanup()
		}
	}()

	msgLogReg, err := messageLogLoadDisabledChannels(db)

	if err != nil {
		log.Fatal("failed to load enabled channels from database:", err)
	}

	msgQueue := NewDBCacheQueue[msgLogCache](db, 1000, 100,
		func(caches []msgLogCache) {
			for _, item := range caches {
				msgLogReg.MsgLogCache.Delete(item.MessageID)
			}
		})
	dgr.RegMsgCreate(bot, []string{"*"}, func(c *dgr.MsgCreateCtx) {
		if c.Args.Author.Bot {
			return
		}
		guildConfig, ok := msgLogReg.GuildConfig.Load(c.Args.GuildID)
		if !ok {
			return
		}
		config := guildConfig.(msgLogGuildSettingConfig)
		if config.Config&GlConfigDelete == 0 && config.Config&GlConfigEdit == 0 {
			return
		}

		logCacheData := msgLogCache{
			MessageID: c.Args.ID,
			Content:   c.Args.Content,
			AuthorID:  c.Args.Author.ID,
			ChannelID: c.Args.ChannelID,
			CreatedAt: c.Args.Timestamp,
		}

		msgLogReg.MsgLogCache.Store(c.Args.ID, logCacheData)

		msgQueue.Push(logCacheData)
	})

	bot.Session.AddHandler(func(s *discordgo.Session, m *discordgo.MessageDelete) {
		if _, disabled := msgLogReg.DisableChannel.Load(m.ChannelID); disabled {
			return
		}

		var cache msgLogCache
		value, ok := msgLogReg.MsgLogCache.Load(m.ID)

		if ok {
			var castOK bool
			cache, castOK = value.(msgLogCache)
			if !castOK {
				return
			}
		} else {
			if err := db.
				Where("message_id = ?", m.ID).
				First(&cache).Error; err != nil {
				return
			}
		}

		guildConfig, ok := msgLogReg.GuildConfig.Load(m.GuildID)
		if !ok {
			return
		}

		config := guildConfig.(msgLogGuildSettingConfig)

		if config.Config&GlConfigDelete == 0 {
			return
		}

		var contentLine []string

		var content string

		contentRow := strings.Split(cache.Content, "\n")

		for _, line := range contentRow {
			contentLine = append(contentLine, fmt.Sprintf("> %s", line))
		}

		content = strings.Join(contentLine, "\n")

		if r := []rune(content); len(r) > 1024 {
			content = string(r[:1021]) + "..."
		}

		var embedAuthor *discordgo.MessageEmbedAuthor

		user, err := s.User(cache.AuthorID)

		if err == nil {
			embedAuthor = &discordgo.MessageEmbedAuthor{
				Name:    user.Username,
				IconURL: user.AvatarURL(""),
			}
		}

		embed := &discordgo.MessageEmbed{
			Author:      embedAuthor,
			Description: fmt.Sprintf("**<@%s> が <#%s> で送信したメッセージが削除されました。**", cache.AuthorID, cache.ChannelID),
			Color:       0xFF0000,
			Fields: []*discordgo.MessageEmbedField{
				{
					Value: content,
				},
			},
			Footer: &discordgo.MessageEmbedFooter{
				Text: fmt.Sprintf("message ID: %s | author ID: %s", m.ID, cache.AuthorID),
			},
			Timestamp: time.Now().Format(time.RFC3339),
		}

		msgLogReg.MsgLogCache.Delete(m.ID)

		db.Model(&msgLogCache{}).
			Where("message_id = ?", m.ID).
			Delete(&msgLogCache{})

		if config.LogChannel != "" {
			_, err := s.ChannelMessageSendEmbed(config.LogChannel, embed)
			if err != nil {
				log.Println("Error sending log embed:", err)
			}
		}
	})

	bot.Session.AddHandler(func(s *discordgo.Session, m *discordgo.MessageUpdate) {
		if _, disabled := msgLogReg.DisableChannel.Load(m.ChannelID); disabled {
			return
		}

		var cache msgLogCache
		value, ok := msgLogReg.MsgLogCache.Load(m.ID)

		if ok {
			var castOK bool
			cache, castOK = value.(msgLogCache)
			if !castOK {
				return
			}
		} else {
			if err := db.
				Where("message_id = ?", m.ID).
				First(&cache).Error; err != nil {
				return
			}
		}

		if m.Content == cache.Content {
			return
		}

		guildConfig, ok := msgLogReg.GuildConfig.Load(m.GuildID)
		if !ok {
			return
		}

		config := guildConfig.(msgLogGuildSettingConfig)

		if config.Config&GlConfigEdit == 0 {
			return
		}

		var prevContentLine []string
		var prevContent string

		prevContentRow := strings.Split(cache.Content, "\n")
		for _, line := range prevContentRow {
			prevContentLine = append(prevContentLine, fmt.Sprintf("> %s", line))
		}
		prevContent = strings.Join(prevContentLine, "\n")

		var contentLine []string
		var content string

		contentRow := strings.Split(m.Content, "\n")
		for _, line := range contentRow {
			contentLine = append(contentLine, fmt.Sprintf("> %s", line))
		}
		content = strings.Join(contentLine, "\n")

		if r := []rune(prevContent); len(r) > 1024 {
			prevContent = string(r[:1021]) + "..."
		}
		if r := []rune(content); len(r) > 1024 {
			content = string(r[:1021]) + "..."
		}

		embed := &discordgo.MessageEmbed{
			Author: &discordgo.MessageEmbedAuthor{
				Name:    m.Author.Username,
				IconURL: m.Author.AvatarURL(""),
			},
			Description: fmt.Sprintf("**<@%s> が <#%s> で送信したメッセージが編集されました**\n-# [メッセージにジャンプ](https://discordapp.com/channels/%s/%s/%s)",
				cache.AuthorID, cache.ChannelID, m.GuildID, m.ChannelID, m.ID),
			Color: 0x0000FF,
			Fields: []*discordgo.MessageEmbedField{
				{
					Name:  "編集前",
					Value: prevContent,
				},
				{
					Name:  "編集後",
					Value: content,
				},
			},
			Footer: &discordgo.MessageEmbedFooter{
				Text: fmt.Sprintf("author ID: %s", cache.AuthorID),
			},
			Timestamp: time.Now().Format(time.RFC3339),
		}

		logCacheData := msgLogCache{
			MessageID: m.ID,
			Content:   m.Content,
			AuthorID:  m.Author.ID,
			ChannelID: m.ChannelID,
			CreatedAt: m.Timestamp,
		}

		msgLogReg.MsgLogCache.Store(m.ID, logCacheData)

		msgQueue.Push(logCacheData)

		if config.LogChannel != "" {
			_, err := s.ChannelMessageSendEmbed(config.LogChannel, embed)
			if err != nil {
				log.Println("Error sending log embed:", err)
			}
		}
	})

	logGroup := dgr.Group(bot, "log", "メッセージログに関する設定")

	logServerGroup := dgr.SubGroup(logGroup, "server", "サーバー全体の設定")

	type logServerEnable struct {
		Channel       *discordgo.Channel `dgr:"channel" desc:"ログを送信するチャンネル" required:"true"`
		ContentDelete bool               `dgr:"deletelog" desc:"削除ログを送信するか否か" required:"true"`
		Edit          bool               `dgr:"editlog" desc:"編集ログを送信するか否か" required:"true"`
	}

	dgr.RegSlash(logServerGroup, "settings", "このサーバーのメッセージログを設定します。", func(c *dgr.Context[logServerEnable]) {
		guildId := c.Interaction.GuildID

		if isAdmin(c) == false {
			err := c.Reply("このサーバーの管理権限が必要です。", dgr.WithEphemeral())
			if err != nil {
				log.Println("Error sending reply:", err)
				return
			}
			return
		}

		var config LogGuildConfig = GlConfigNone

		if c.Args.ContentDelete {
			config |= GlConfigDelete
		}
		if c.Args.Edit {
			config |= GlConfigEdit
		}

		err := db.Clauses(clause.OnConflict{
			UpdateAll: true,
		}).Create(&msgLogGuildSetting{
			GuildID:    guildId,
			LogChannel: c.Args.Channel.ID,
			Config:     config,
		}).Error

		if err != nil {
			log.Println("Error inserting into database:", err)
			err := c.Reply("エラーが発生しました。", dgr.WithEphemeral())
			if err != nil {
				log.Println("Error sending reply:", err)
				return
			}
			return
		}
		msgLogReg.GuildConfig.Store(guildId, msgLogGuildSettingConfig{
			LogChannel: c.Args.Channel.ID,
			Config:     config,
		})

		configUpdateEmbed := &discordgo.MessageEmbed{
			Fields: []*discordgo.MessageEmbedField{
				{
					Name:  "ログチャンネル",
					Value: c.Args.Channel.Mention(),
				},
				{
					Name: "削除ログを表示？",
					Value: func() string {
						if c.Args.ContentDelete {
							return "はい"
						}
						return "いいえ"
					}(),
				},
				{
					Name: "編集ログを表示？",
					Value: func() string {
						if c.Args.Edit {
							return "はい"
						}
						return "いいえ"
					}(),
				},
			},
		}

		err = c.Reply("メッセージログの設定が更新されました。", dgr.WithEphemeral(), dgr.WithEmbeds(configUpdateEmbed))
		if err != nil {
			log.Println("Error sending reply:", err)
			return
		}

	})

	type logServerConfig struct {
		Channel       *discordgo.Channel `dgr:"channel" desc:"ログを送信するチャンネル"`
		ContentDelete bool               `dgr:"deletelog" desc:"削除ログを送信するか否か"`
		Edit          bool               `dgr:"editlog" desc:"編集ログを送信するか否か"`
		AssetsDelete  bool               `dgr:"assetlog" desc:"添付ファイルの削除ログを送信するか否か"`
	}

	dgr.RegSlash(logServerGroup, "disable", "このサーバーのメッセージログを無効にします。", func(c *dgr.Context[struct{}]) {
		guildId := c.Interaction.GuildID

		if isAdmin(c) == false {
			err := c.Reply("このサーバーの管理権限が必要です。", dgr.WithEphemeral())
			if err != nil {
				log.Println("Error sending reply:", err)
				return
			}
			return
		}

		var count int64

		err := db.Model(&msgLogGuildSetting{}).
			Where("guild_id = ?", guildId).
			Count(&count).Error

		if err != nil {
			log.Println("Error querying database:", err)
			err := c.Reply("エラーが発生しました。", dgr.WithEphemeral())
			if err != nil {
				log.Println("Error sending reply:", err)
				return
			}
			return
		}

		if count == 0 {
			err := c.Reply("メッセージログはすでに無効です。", dgr.WithEphemeral())
			if err != nil {
				log.Println("Error sending reply:", err)
				return
			}
		} else {
			db.Model(&msgLogGuildSetting{}).
				Where("guild_id = ?", guildId).
				Delete(&msgLogGuildSetting{})
			msgLogReg.GuildConfig.Delete(guildId)
			err := c.Reply("メッセージログを無効にしました。", dgr.WithEphemeral())
			if err != nil {
				log.Println("Error sending reply:", err)
				return
			}
		}
	})

	logChannnelGroup := dgr.SubGroup(logGroup, "channel", "チャンネル単位の設定")

	dgr.RegSlash(logChannnelGroup, "enable", "このチャンネルのメッセージログを有効にします。", func(c *dgr.Context[struct{}]) {
		if !isAdmin(c) {
			_ = c.Reply("このサーバーの管理権限が必要です。", dgr.WithEphemeral())
			return
		}

		channelID := c.Interaction.ChannelID

		result := db.Where("channel_id = ?", channelID).Delete(&msgLogChannelSetting{})
		if result.Error != nil {
			log.Println("Error deleting channel setting:", result.Error)
			_ = c.Reply("エラーが発生しました。", dgr.WithEphemeral())
			return
		}

		msgLogReg.DisableChannel.Delete(channelID)

		if result.RowsAffected == 0 {
			_ = c.Reply("このチャンネルのメッセージログはすでに有効です。", dgr.WithEphemeral())
		} else {
			_ = c.Reply("このチャンネルのメッセージログを有効にしました。", dgr.WithEphemeral())
		}
	})
	dgr.RegSlash(logChannnelGroup, "disable", "このチャンネルのメッセージログを無効にします。", func(c *dgr.Context[struct{}]) {
		if !isAdmin(c) {
			_ = c.Reply("このサーバーの管理権限が必要です。", dgr.WithEphemeral())
			return
		}

		channelID := c.Interaction.ChannelID

		var count int64
		err := db.Model(&msgLogChannelSetting{}).Where("channel_id = ?", channelID).Count(&count).Error
		if err != nil {
			log.Println("Error querying database:", err)
			_ = c.Reply("エラーが発生しました。", dgr.WithEphemeral())
			return
		}

		if count > 0 {
			_ = c.Reply("このチャンネルのメッセージログはすでに無効です。", dgr.WithEphemeral())
			return
		}

		err = db.Create(&msgLogChannelSetting{
			ChannelID: channelID,
			GuildID:   c.Interaction.GuildID,
		}).Error

		if err != nil {
			log.Println("Error creating channel setting:", err)
			_ = c.Reply("エラーが発生しました。", dgr.WithEphemeral())
			return
		}

		msgLogReg.DisableChannel.Store(channelID, c.Interaction.GuildID)
		_ = c.Reply("このチャンネルのメッセージログを無効にしました。", dgr.WithEphemeral())
	})
}
