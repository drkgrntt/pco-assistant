package controllers

import (
	"pco-assistant/internal/server/middleware"
	"pco-assistant/internal/views"

	"github.com/gofiber/fiber/v2"
)

type HomeController struct{}

func NewHomeController() *HomeController {
	return &HomeController{}
}

func (hc *HomeController) RegisterRoutes(router fiber.Router) {
	router.Get("/", hc.Index)
}

func (hc *HomeController) Index(c *fiber.Ctx) error {
	_, personName := middleware.CurrentPerson(c)
	return render(c, views.Home(personName))
}
