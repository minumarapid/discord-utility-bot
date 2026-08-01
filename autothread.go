package main

import (
	"log"
	"sync"

	"time"

	"github.com/bwmarrin/discordgo"
	dgr "github.com/minumarapid/discord-go-router"
	"gorm.io/gorm"
)

type ChannelSetting struct {
	ChannelID string `gorm:"primaryKey"`
	GuildID   string `gorm:"index"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

type ChannelRegistry struct {
	Enable sync.Map // key: channelID string, value: guildID string
}

func LoadEnabledChannels(db *gorm.DB) (*ChannelRegistry, error) {
	var rows []ChannelSetting
	err := db.Model(&ChannelSetting{}).Find(&rows).Error
	if err != nil {
		return nil, err
	}

	registry := &ChannelRegistry{}

	for _, row := range rows {
		registry.Enable.Store(row.ChannelID, row.GuildID)
	}

	return registry, nil
}

func autothread(bot *dgr.Dgr, db *gorm.DB) {
	db.AutoMigrate(&ChannelSetting{})

	autoThreadGroup := dgr.Group(bot, "autothread", "自動スレッド作成に関する設定")

	channelReg, err := LoadEnabledChannels(db)

	if err != nil {
		log.Fatal("failed to load enabled channels from database:", err)
	}

	dgr.RegSlash(autoThreadGroup, "set", "実行したチャンネルで自動スレッドを有効にします。", func(c *dgr.Context[struct{}]) {
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

		err := db.Model(&ChannelSetting{}).
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
			db.Model(&ChannelSetting{}).
				Create(&ChannelSetting{
					ChannelID: channelID,
					GuildID:   c.Interaction.GuildID,
				})
			channelReg.Enable.Store(channelID, c.Interaction.GuildID)
			err := c.Reply("自動スレッドが有効になりました。", dgr.WithEphemeral())
			if err != nil {
				log.Println("Error sending reply:", err)
				return
			}
		} else {
			err := c.Reply("自動スレッドはすでに有効です。", dgr.WithEphemeral())
			if err != nil {
				log.Println("Error sending reply:", err)
				return
			}
		}
	})

	dgr.RegSlash(autoThreadGroup, "unset", "実行したチャンネルで自動スレッドを無効にします。", func(c *dgr.Context[struct{}]) {
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

		err = db.Model(&ChannelSetting{}).
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
			err := c.Reply("自動スレッドはすでに無効です。", dgr.WithEphemeral())
			if err != nil {
				log.Println("Error sending reply:", err)
				return
			}
		} else {
			db.Model(&ChannelSetting{}).
				Where("channel_id = ?", channelID).
				Delete(&ChannelSetting{})
			channelReg.Enable.Delete(channelID)
			err := c.Reply("自動スレッドが無効になりました。", dgr.WithEphemeral())
			if err != nil {
				log.Println("Error sending reply:", err)
				return
			}
		}
	})

	// TODO いつか作る
	//dgr.RegSlash(autoThreadGroup, "list", "自動スレッドが設定されたチャンネルを確認します。", func(c *dgr.Context[struct{}]) {
	//
	//})

	dgr.RegMsgCreate(bot, []string{"*"}, func(c *dgr.MsgCreateCtx){
		channelID := c.Args.ChannelID
		_, ok := channelReg.Enable.Load(channelID)
		if !ok {
			return
		}
		var threadName string
		threadName = "返信用"
			_, err := c.Session.MessageThreadStart(
			c.Args.ChannelID,
			c.Args.ID,
			threadName,
			4320,
		)
		if err != nil {
			log.Println("Error starting thread:", err)
			return
		}
	})
}
