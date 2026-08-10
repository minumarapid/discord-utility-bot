package msglog

import (
	"log"
	"time"

	"github.com/bwmarrin/discordgo"
)

type LogJob struct {
	ChannelID string
	Embed     *discordgo.MessageEmbed
}

type LogQueue struct {
	jobs chan LogJob
}

func NewLogQueue(bufferSize int, workerCount int, s *discordgo.Session) *LogQueue {
	q := &LogQueue{
		jobs: make(chan LogJob, bufferSize),
	}

	for i := 0; i < workerCount; i++ {
		go func() {
			for job := range q.jobs {
				_, err := s.ChannelMessageSendEmbed(job.ChannelID, job.Embed)
				if err != nil {
					log.Println("Failed to send log embed:", err)
				}
				time.Sleep(250 * time.Millisecond)
			}
		}()
	}

	return q
}

func (q *LogQueue) Enqueue(job LogJob) {
	select {
	case q.jobs <- job:
	default:
		log.Println("Log queue is full! Dropping log job.") // バッファオーバーフロー対策
	}
}
