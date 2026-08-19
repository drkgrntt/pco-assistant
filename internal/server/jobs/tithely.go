package jobs

import (
	"log"
	"time"

	"church/internal/lib"

	"github.com/go-co-op/gocron/v2"
)

var scheduleDuration = time.Hour * time.Duration(1)

type TithelyJobs struct{}

func NewTithelyJobs() *TithelyJobs {
	return &TithelyJobs{}
}

func (j *TithelyJobs) Register(scheduler gocron.Scheduler) {
	_, err := scheduler.NewJob(
		gocron.DurationJob(scheduleDuration),
		gocron.NewTask(j.processPeopleToPCO),
	)
	if err != nil {
		log.Println("Error registering tithely->pco cron", err.Error())
	}
}

func (j *TithelyJobs) processPeopleToPCO() {
	processed, errors := lib.ProcessTithelyPeopleToPCO()

	log.Println("processed", processed)
	log.Println("errors", errors)
}
