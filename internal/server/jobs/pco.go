package jobs

import "github.com/go-co-op/gocron/v2"

type PCOJobs struct{}

func NewPCOJobs() *PCOJobs {
	return &PCOJobs{}
}

// Register is a placeholder for PCO-specific scheduled jobs (e.g. syncing
// data out of PCO on a schedule). Empty for now, mirroring AuthJobs so new
// jobs have an obvious home.
func (j *PCOJobs) Register(scheduler gocron.Scheduler) {
}
