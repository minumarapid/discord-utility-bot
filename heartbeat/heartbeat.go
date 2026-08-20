// Package heartbeat Health check using heatbeat
package heartbeat

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"
)

func Heartbeat(ctx context.Context, url string, interval time.Duration) {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	if err := request(client, url); err != nil {
		log.Printf("[Heartbeat Error] %v", err)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			{
				return
			}
		case <-ticker.C:
			if err := request(client, url); err != nil {
				log.Printf("[Heartbeat Error] %v", err)
			}
		}
	}
}

func request(client *http.Client, url string) error {
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	return nil
}
