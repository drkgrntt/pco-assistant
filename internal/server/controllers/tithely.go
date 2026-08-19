package controllers

import (
	"church/internal/lib"
	"church/tithely"

	"github.com/gofiber/fiber/v2"
)

type TithelyController struct{}

func NewTithelyController() *TithelyController {
	return &TithelyController{}
}

func (tc *TithelyController) RegisterRoutes(router fiber.Router) {
	group := router.Group("/tithely")

	group.Get("/", tc.Index)
	group.Get("/process", tc.Process)
}

func (tc *TithelyController) Index(c *fiber.Ctx) error {
	data, _ := tithely.GetAccounts(&tithely.AccountsParams{
		Limit: 20,
	})

	return c.JSON(data)
}

// Process syncs Tithely accounts into PCO as people, matching the scheduled
// job in jobs.TithelyJobs so it can also be triggered on demand.
func (tc *TithelyController) Process(c *fiber.Ctx) error {
	processed, errors := lib.ProcessTithelyPeopleToPCO()

	return c.JSON(map[string]any{
		"processed": processed,
		"errors":    errors,
	})
}
