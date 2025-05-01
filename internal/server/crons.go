package server

import (
	"church/internal/lib"
	"log"
	"time"

	"github.com/go-co-op/gocron/v2"
)

var scheduleDuration = time.Hour * time.Duration(1)

func (s *FiberServer) RegisterCrons() {
	scheduler, err := gocron.NewScheduler()
	if err != nil {
		log.Println("Error starting scheduler", err.Error())
	}

	// Create the next time
	_, err = scheduler.NewJob(
		gocron.DurationJob(scheduleDuration),
		gocron.NewTask(processTithelyPeopleToPCO),
	)
	if err != nil {
		log.Println("Error starting scheduler", err.Error())
	}

	scheduler.Start()

	log.Println("Crons registered")
}

func processTithelyPeopleToPCO() {
	processed, errors := lib.ProcessTithelyPeopleToPCO()

	log.Println("processed", processed)
	log.Println("errors", errors)
}
