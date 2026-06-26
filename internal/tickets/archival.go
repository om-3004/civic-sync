package tickets

import (
	"context"
	"log"
	"time"

	"github.com/civic-sync/civic-sync/internal/store"
)

const archivalInterval = 15 * time.Minute

// StartArchivalScheduler launches a background goroutine that periodically
// archives expired tickets. It ticks every 15 minutes and calls
// store.ArchiveExpiredTickets on each tick.
//
// The goroutine stops cleanly when the provided context is cancelled —
// this happens naturally when the Cloud Run instance scales to zero.
//
// Requirements: 9.2
func StartArchivalScheduler(ctx context.Context, s store.Store) {
	go func() {
		ticker := time.NewTicker(archivalInterval)
		defer ticker.Stop()

		log.Println("archival: scheduler started, interval=15m")

		for {
			select {
			case <-ctx.Done():
				log.Println("archival: scheduler stopped (context cancelled)")
				return
			case t := <-ticker.C:
				log.Printf("archival: running at %s", t.UTC().Format(time.RFC3339))
				if err := s.ArchiveExpiredTickets(ctx); err != nil {
					log.Printf("archival: error archiving tickets: %v", err)
				} else {
					log.Println("archival: completed successfully")
				}
			}
		}
	}()
}
