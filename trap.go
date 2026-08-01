package main

import (
	"log"
	"sync"

	"time"

	"github.com/bwmarrin/discordgo"
	dgr "github.com/minumarapid/discord-go-router"
	"gorm.io/gorm"
)

type trapChannelSetting struct {
	ChannelID string `gorm:"primaryKey"`
	GuildID   string `gorm:"index"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

type trapChannelRegistry struct {
	Enable sync.Map // key: channelID string, value: guildID string
}

func trapLoadEnabledChannels(db *gorm.DB) (*trapChannelRegistry, error) {
	var rows []trapChannelSetting
	err := db.Model(&trapChannelSetting{}).Find(&rows).Error
	if err != nil {
		return nil, err
	}

	registry := &trapChannelRegistry{}

	for _, row := range rows {
		registry.Enable.Store(row.ChannelID, row.GuildID)
	}

	return registry, nil
}

func trapchannel(bot *dgr.Dgr, db *gorm.DB) {
	db.AutoMigrate(&trapChannelSetting{})

	trapChannelGroup := dgr.Group(bot, "trap", "トラップチャンネルに関する設定")

	trapChannelReg, err := trapLoadEnabledChannels(db)

	if err != nil {
		log.Fatal("failed to load enabled channels from database:", err)
	}

	dgr.RegSlash(trapChannelGroup, "set", "実行したチャンネルでトラップチャンネルを有効にします。", func(c *dgr.Context[struct{}]) {
		channelID := c.Interaction.ChannelID

		perms := c.Interaction.Member.Permissions

		if perms&discordgo.PermissionManageChannels == 0 {
			err := c.Reply("このチャンネルの管理権限が必要です。", dgr.WithEphemeral())
			if err != nil {
				log.Println("Error sending reply:", err)
				return
			}
			return
		}

		var count int64

		err := db.Model(&trapChannelSetting{}).
			Where("channel_id = ?", channelID).
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
			db.Model(&trapChannelSetting{}).
				Create(&trapChannelSetting{
					ChannelID: channelID,
					GuildID:   c.Interaction.GuildID,
				})
			trapChannelReg.Enable.Store(channelID, c.Interaction.GuildID)
			err := c.Reply("トラップチャンネルが有効になりました。", dgr.WithEphemeral())
			if err != nil {
				log.Println("Error sending reply:", err)
				return
			}
		} else {
			err := c.Reply("トラップチャンネルはすでに有効です。", dgr.WithEphemeral())
			if err != nil {
				log.Println("Error sending reply:", err)
				return
			}
		}
	})

	dgr.RegSlash(trapChannelGroup, "unset", "実行したチャンネルでトラップチャンネルを無効にします。", func(c *dgr.Context[struct{}]) {
		channelID := c.Interaction.ChannelID

		perms := c.Interaction.Member.Permissions

		if perms&discordgo.PermissionManageChannels == 0 {
			err := c.Reply("このチャンネルの管理権限が必要です。", dgr.WithEphemeral())
			if err != nil {
				log.Println("Error sending reply:", err)
				return
			}
			return
		}

		var count int64

		err = db.Model(&trapChannelSetting{}).
			Where("channel_id = ?", channelID).
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
			err := c.Reply("トラップチャンネルはすでに無効です。", dgr.WithEphemeral())
			if err != nil {
				log.Println("Error sending reply:", err)
				return
			}
		} else {
			db.Model(&trapChannelSetting{}).
				Where("channel_id = ?", channelID).
				Delete(&trapChannelSetting{})
			trapChannelReg.Enable.Delete(channelID)
			err := c.Reply("トラップチャンネルが無効になりました。", dgr.WithEphemeral())
			if err != nil {
				log.Println("Error sending reply:", err)
				return
			}
		}
	})

	dgr.RegMsgCreate(bot, []string{"*"}, func(c *dgr.MsgCreateCtx) {
		channelID := c.Args.ChannelID
		_, ok := trapChannelReg.Enable.Load(channelID)
		if !ok {
			return
		}
		err := c.Session.ChannelMessageDelete(channelID, c.Args.ID)
		if err != nil {
			log.Println("Error deleting message:", err)
			return
		}
		timeoutDuration := time.Now().Add(10 * 24 * time.Hour)
		err = c.Session.GuildMemberTimeout(c.Args.GuildID, c.Args.Author.ID, &timeoutDuration)
		if err != nil {
			log.Println("Error timing out user:", err)
			return
		}
	})
}
