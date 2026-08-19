package server

import (
	"github.com/gofiber/fiber/v2"

	"pco-assistant/internal/auth"
	"pco-assistant/internal/database"
	"pco-assistant/internal/scheduling"
)

type FiberServer struct {
	*fiber.App

	db              database.Service
	schedulingStore *scheduling.Store
	authStore       *auth.Store
}

func New() *FiberServer {
	db := database.New()

	server := &FiberServer{
		App: fiber.New(fiber.Config{
			ServerHeader: "pco-assistant",
			AppName:      "pco-assistant",
		}),

		db:              db,
		schedulingStore: scheduling.NewStore(db.DB()),
		authStore:       auth.NewStore(db.DB()),
	}

	return server
}
