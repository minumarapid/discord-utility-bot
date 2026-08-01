package main

import (
	"log"

	"github.com/bwmarrin/discordgo"
	dgr "github.com/minumarapid/discord-go-router"
)

func threadpin(bot *dgr.Dgr) {
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
}
