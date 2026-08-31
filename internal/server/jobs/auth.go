package jobs

import (
	"context"
	"time"

	"pco-assistant/internal/auth"
	"pco-assistant/internal/logger"

	"github.com/go-co-op/gocron/v2"
)

var sessionCleanupInterval = 24 * time.Hour

type AuthJobs struct {
	store *auth.Store
}

func NewAuthJobs(store *auth.Store) *AuthJobs {
	return &AuthJobs{store: store}
}

// Register purges expired sessions daily so the table doesn't grow forever.
func (j *AuthJobs) Register(scheduler gocron.Scheduler) {
	_, err := scheduler.NewJob(
		gocron.DurationJob(sessionCleanupInterval),
		gocron.NewTask(j.deleteExpiredSessions),
	)
	if err != nil {
		logger.Error("registering session cleanup cron failed", "error", err)
	}
}

func (j *AuthJobs) deleteExpiredSessions() {
	deleted, err := j.store.DeleteExpiredSessions(context.Background())
	if err != nil {
		logger.Error("deleting expired sessions failed", "error", err)
		return
	}
	if deleted > 0 {
		logger.Info("deleted expired sessions", "count", deleted)
	}
}
