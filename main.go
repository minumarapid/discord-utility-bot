package main

import (
	"log"
	"os"

	"github.com/glebarez/sqlite"
	"github.com/joho/godotenv"
	dgr "github.com/minumarapid/discord-go-router"
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

	db, err := gorm.Open(sqlite.Open("sqlite.db"), &gorm.Config{})
	if err != nil {
		log.Fatal("failed to connect database")
	}

	threadpin(bot)

	autothread(bot, db)

	trapchannel(bot, db)

	bot.Run("")
}
