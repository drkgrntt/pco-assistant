package server

import (
	"github.com/gofiber/fiber/v2"

	"church/internal/auth"
	"church/internal/database"
	"church/internal/scheduling"
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
			ServerHeader: "church",
			AppName:      "church",
		}),

		db:              db,
		schedulingStore: scheduling.NewStore(db.DB()),
		authStore:       auth.NewStore(db.DB()),
	}

	return server
}
