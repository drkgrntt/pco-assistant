package server

import (
	"log"

	"pco-assistant/internal/server/jobs"

	"github.com/go-co-op/gocron/v2"
)

func (s *FiberServer) RegisterCrons() {
	scheduler, err := gocron.NewScheduler()
	if err != nil {
		log.Println("Error starting scheduler", err.Error())
		return
	}

	jobs.NewPCOJobs().Register(scheduler)
	jobs.NewAuthJobs(s.authStore).Register(scheduler)
	jobs.NewBillingJobs(s.authStore, s.billingClient).Register(scheduler)
	jobs.NewBackupJobs().Register(scheduler)

	scheduler.Start()

	log.Println("Crons registered")
}
