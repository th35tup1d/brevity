package service

import (
	"context"
	"encoding/json"
	"log"
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

func StartAnalyticsConsumer(brokerAddress string, db *pgxpool.Pool) {
    r := kafka.NewReader(kafka.ReaderConfig{
        Brokers:  []string{brokerAddress},
        Topic:    "url-clicks",
        GroupID:  "analytics-service-group",
        MinBytes: 10e3,
        MaxBytes: 1e6,
    })

    go func() {
        defer r.Close()
        ctx := context.Background()
        for {
            m, err := r.FetchMessage(ctx)
            if err != nil {
                log.Printf("--> ⚠️ [KAFKA CONSUMER] Error fetching message: %v", err)
                break
            }

            var event models.ClickEvent
            if err := json.Unmarshal(m.Value, &event); err != nil {
                log.Printf("--> ⚠️ [KAFKA CONSUMER] Failed to unmarshal event: %v", err)
                continue
            }

            // 1. Look up the url_id from the urls table using the short_code
            var urlID int64
            err = db.QueryRow(ctx, "SELECT id FROM urls WHERE short_code = $1", event.ShortCode).Scan(&urlID)
            if err != nil {
                log.Printf("--> ⚠️ [KAFKA CONSUMER] Short code '%s' not found, skipping analytics: %v", event.ShortCode, err)
                continue
            }

            // 2. Insert the rich click event data into click_events
            query := `INSERT INTO click_events (url_id, ip_address, user_agent, clicked_at, referrer, country, device) VALUES ($1, $2, $3, $4, $5, $6, $7)`
            _, err = db.Exec(ctx, query, urlID, event.IPAddress, event.UserAgent, event.Timestamp, event.Referrer, event.Country, event.Device)
            if err != nil {
                log.Printf("--> ⚠️ [DB ERROR] Failed to save analytics for '%s': %v", event.ShortCode, err)
                continue
            }

            // 3. Commit message offset
            if err := r.CommitMessages(ctx, m); err != nil {
                log.Printf("--> ⚠️ [KAFKA CONSUMER] Failed to commit offset: %v", err)
            }
        }
    }()
}
