package jobs

import (
	"context"
	"log"
	"time"

	"pco-assistant/internal/auth"

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
		log.Println("Error registering session cleanup cron", err.Error())
	}
}

func (j *AuthJobs) deleteExpiredSessions() {
	deleted, err := j.store.DeleteExpiredSessions(context.Background())
	if err != nil {
		log.Println("Error deleting expired sessions:", err)
		return
	}
	if deleted > 0 {
		log.Println("Deleted expired sessions:", deleted)
	}
}
