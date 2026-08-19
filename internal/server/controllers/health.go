package controllers

import (
	"pco-assistant/internal/database"

	"github.com/gofiber/fiber/v2"
)

type HealthController struct {
	db database.Service
}

func NewHealthController(db database.Service) *HealthController {
	return &HealthController{db: db}
}

func (hc *HealthController) RegisterRoutes(router fiber.Router) {
	router.Get("/health", hc.Index)
}

func (hc *HealthController) Index(c *fiber.Ctx) error {
	return c.JSON(hc.db.Health())
}
