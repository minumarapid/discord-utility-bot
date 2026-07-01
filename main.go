package main

import (
	"log"
	"os"
	"sync"

	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/glebarez/sqlite"
	"github.com/joho/godotenv"
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

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}
	token := os.Getenv("BOT_TOKEN")

	bot, err := dgr.New(token)
	if err != nil {
		log.Fatal("Error creating bot instance")
	}

	db, err := gorm.Open(sqlite.Open("sqlite.db"), &gorm.Config{})
	if err != nil {
		log.Fatal("failed to connect database")
	}
	db.AutoMigrate(&ChannelSetting{})

	channelReg, err := LoadEnabledChannels(db)

	if err != nil {
		log.Fatal("failed to load enabled channels from database:", err)
	}

	dgr.RegMessageCtx(bot, "ピン留め(解除)", func(c *dgr.Context[discordgo.Message]) {
		interactionUser := c.Interaction.Member
		channel, err := c.Session.Channel(c.Args.ChannelID)
		if err != nil {
			log.Println("Error fetching channel:", err)
			return
		}
		if channel.IsThread() != true {
			c.Reply("スレッド内で実行する必要があります。", dgr.WithEphemeral())
			return
		}
		threadMaster := channel.OwnerID
		if interactionUser.User.ID != threadMaster {
			c.Reply("スレッドの作成者のみがピン留めを操作できます。", dgr.WithEphemeral())
			return
		}
		if !c.Args.Pinned {
			err = c.Session.ChannelMessagePin(channel.ID, c.Args.ID)
		} else {
			err = c.Session.ChannelMessageUnpin(channel.ID, c.Args.ID)
		}
		if err != nil {
			log.Println("Error pinning/unpinning message:", err)
			c.Reply("ピン留めの操作中にエラーが発生しました。", dgr.WithEphemeral())
		}
		c.Reply("ピン留めの操作が完了しました。", dgr.WithEphemeral())
	})

	autoThreadGroup := dgr.Group(bot, "autothread", "自動スレッド作成に関する設定")

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

	bot.Session.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		channelID := m.ChannelID
		_, ok := channelReg.Enable.Load(channelID)
		if !ok {
			return
		}
		var threadName string
		//if m.Content == "" {
		//	threadName = "返信用"
		//} else {
		//	threadName = m.Content
		//}
		threadName = "返信用"
		_, err = s.MessageThreadStart(
			m.ChannelID,
			m.ID,
			threadName,
			4320,
		)
		if err != nil {
			log.Println("Error starting thread:", err)
			return
		}
	})

	bot.Run("")
}
