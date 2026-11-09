package service

import (
	"context"
	"encoding/json"
	"log"
	"time"
	"sync"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/segmentio/kafka-go"
	"brevity/internal/repository"
	"brevity/internal/models"
)

type ClickPayload struct {
	ShortCode string
	IPAddress string
	UserAgent string
}

type AnalyticsWorkerPool struct {
	repo    *repository.URLRepository
	clickChan chan ClickPayload
	wg      sync.WaitGroup
}

func NewAnalyticsWorkerPool(repo *repository.URLRepository, bufferSize int, workerCount int) *AnalyticsWorkerPool {
	pool := &AnalyticsWorkerPool{
		repo:      repo,
		clickChan: make(chan ClickPayload, bufferSize),
	}

	// Start background workers
	for i := 0; i < workerCount; i++ {
		pool.wg.Add(1)
		go pool.worker()
	}

	return pool
}

// EnqueueClick non-blockingly pushes a click event to the channel
func (p *AnalyticsWorkerPool) EnqueueClick(payload ClickPayload) {
	select {
	case p.clickChan <- payload:
	default:
		// If channel buffer is full under extreme traffic, drop gracefully to protect memory
		log.Println("Analytics buffer full, dropping click event")
	}
}

func (p *AnalyticsWorkerPool) worker() {
	defer p.wg.Done()
	ctx := context.Background()

	for payload := range p.clickChan {
		// Fetch URL ID
		urlID, err := p.repo.GetURLIDByShortCode(ctx, payload.ShortCode)
		if err != nil {
			continue // URL might have been deleted or invalid
		}

		// Save to PostgreSQL asynchronously
		_ = p.repo.RecordClick(ctx, urlID, payload.IPAddress, payload.UserAgent)
	}
}

func (p *AnalyticsWorkerPool) Stop() {
	close(p.clickChan)
	p.wg.Wait()
}

func StartAnalyticsConsumerWithDialer(brokerURL string, db *pgxpool.Pool, dialer *kafka.Dialer) error {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:  []string{brokerURL},
		Topic:    "click-events",
		GroupID:  "brevity-analytics-group",
		Dialer:   dialer,
		MinBytes: 10e3, // 10KB
		MaxBytes: 10e6, // 10MB
	})
	defer reader.Close()

	log.Println("--> 🎧 Kafka analytics consumer started successfully (with custom dialer).")

	ctx := context.Background()
	for {
		msg, err := reader.ReadMessage(ctx)
		if err != nil {
			log.Printf("⚠️ [KAFKA CONSUMER WARNING] Connection error (retrying in 3s): %v", err)
			time.Sleep(3 * time.Second)
			continue
		}

		log.Printf("📥 Received click event message: %s", string(msg.Value))

		var event models.ClickEvent
		if err := json.Unmarshal(msg.Value, &event); err != nil {
			log.Printf("--> ⚠️ [KAFKA CONSUMER] Failed to unmarshal click event: %v", err)
			continue
		}

		query := `
			INSERT INTO click_events (url_id, user_agent, ip_address, referrer, country, device, clicked_at)
			SELECT id, $2, $3, $4, $5, $6, $7 FROM urls WHERE short_code = $1
		`

		tag, err := db.Exec(ctx, query,
			event.ShortCode,
			event.UserAgent,
			event.IPAddress,
			event.Referrer,
			event.Country,
			event.Device,
			event.Timestamp,
		)
		if err != nil {
			log.Printf("--> ❌ [DB ERROR] Failed to record click event for '%s': %v", event.ShortCode, err)
			continue
		}

		if tag.RowsAffected() == 0 {
			log.Printf("--> ⚠️ [DB WARNING] Short code '%s' not found in database, click event skipped", event.ShortCode)
			continue
		}

		log.Printf("--> ✅ [ANALYTICS] Successfully recorded click event for '%s'", event.ShortCode)
	}
}

