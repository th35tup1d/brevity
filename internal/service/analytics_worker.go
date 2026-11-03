package service

import (
	"context"
	"log"
	"sync"

	"brevity/internal/repository"
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
