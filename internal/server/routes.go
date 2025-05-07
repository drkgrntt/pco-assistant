package server

import (
	"church/internal/lib"
	"church/pco"
	"church/tithely"
	"log"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
)

func (s *FiberServer) RegisterFiberRoutes() {
	// Apply CORS middleware
	s.App.Use(cors.New(cors.Config{
		AllowOrigins:     "*",
		AllowMethods:     "GET,POST,PUT,DELETE,OPTIONS,PATCH",
		AllowHeaders:     "Accept,Authorization,Content-Type",
		AllowCredentials: false, // credentials require explicit origins
		MaxAge:           300,
	}))

	s.App.Get("/", s.HelloWorldHandler)

	s.App.Get("/health", s.healthHandler)

	s.App.Get("/process", s.process)
	s.App.Get("/pco", s.pco)
	s.App.Get("/tithely", s.tithely)

	log.Println("Routes registered")
}

func (s *FiberServer) process(c *fiber.Ctx) error {
	processed, errors := lib.ProcessTithelyPeopleToPCO()

	return c.JSON(map[string]any{
		"processed": processed,
		"errors":    errors,
	})
}

func (s *FiberServer) tithely(c *fiber.Ctx) error {
	data, _ := tithely.GetAccounts(&tithely.AccountsParams{
		Limit: 20,
	})

	return c.JSON(data)
}

func (s *FiberServer) pco(c *fiber.Ctx) error {
	people, _ := pco.GetPeople(&pco.PeopleParams{
		Email: "yourgiving.co",
	})

	return c.JSON(people)
}

func (s *FiberServer) HelloWorldHandler(c *fiber.Ctx) error {
	resp := fiber.Map{
		"message": "Hello World",
	}

	return c.JSON(resp)
}

func (s *FiberServer) healthHandler(c *fiber.Ctx) error {
	return c.JSON(s.db.Health())
}
