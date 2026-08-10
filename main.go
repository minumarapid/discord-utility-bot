package main

import (
	"log"
	"os"

	"github.com/bwmarrin/discordgo"
	"github.com/glebarez/sqlite"
	"github.com/joho/godotenv"
	dgr "github.com/minumarapid/discord-go-router"
	msglog "github.com/minumarapid/discord-utility-bot/log"
	"gorm.io/gorm"
)

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
	
	bot.Session.Identify.Intents |= discordgo.IntentsGuildMessages | discordgo.IntentMessageContent

	db, err := gorm.Open(sqlite.Open("sqlite.db"), &gorm.Config{})
	if err != nil {
		log.Fatal("failed to connect database")
	}

	threadpin(bot)

	autothread(bot, db)

	trapchannel(bot, db)

	msglog.MessageLog(bot, db)

	bot.Run("")
}
