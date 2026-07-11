package handlers

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"go_admin/models"
)

func TestRunCrawlerMatchWorkersUsesBoundedConcurrency(t *testing.T) {
	matches := make([]models.Money, 18)
	for index := range matches {
		matches[index].MatchID = fmt.Sprintf("match-%d", index)
	}

	var active int32
	var maximum int32
	startedAt := time.Now()
	runCrawlerMatchWorkers(matches, func(string) {
		current := atomic.AddInt32(&active, 1)
		for {
			observed := atomic.LoadInt32(&maximum)
			if current <= observed || atomic.CompareAndSwapInt32(&maximum, observed, current) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		atomic.AddInt32(&active, -1)
	})

	if maximum < 2 {
		t.Fatalf("expected concurrent workers, maximum active workers was %d", maximum)
	}
	if maximum > crawlerDetailWorkers {
		t.Fatalf("worker limit exceeded: got %d, limit %d", maximum, crawlerDetailWorkers)
	}
	if elapsed := time.Since(startedAt); elapsed >= 300*time.Millisecond {
		t.Fatalf("worker pool did not improve serial execution time: %s", elapsed)
	}
}

func TestCrawlerRateLimiterKeepsRequestInterval(t *testing.T) {
	limiter := crawlerRateLimiter{}
	startedAt := time.Now()
	limiter.Wait("history")
	limiter.Wait("history")
	limiter.Wait("history")

	minimum := crawlerRequestInterval * 2
	if elapsed := time.Since(startedAt); elapsed+20*time.Millisecond < minimum {
		t.Fatalf("request interval was not preserved: elapsed %s, expected at least %s", elapsed, minimum)
	}
}

func TestCrawlerRateLimiterAllowsDifferentRequestTypes(t *testing.T) {
	limiter := crawlerRateLimiter{}
	startedAt := time.Now()
	limiter.Wait("history")
	limiter.Wait("odds_euro")
	limiter.Wait("odds_pankou")

	if elapsed := time.Since(startedAt); elapsed >= 200*time.Millisecond {
		t.Fatalf("different request types should not block each other: %s", elapsed)
	}
}
