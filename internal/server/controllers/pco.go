package controllers

import (
	"strings"

	"pco-assistant/internal/server/middleware"

	pco "github.com/drkgrntt/go-pco"
	"github.com/gofiber/fiber/v2"
)

type PCOController struct{}

func NewPCOController() *PCOController {
	return &PCOController{}
}

func (pc *PCOController) RegisterRoutes(router fiber.Router) {
	// Everything under /pco is a development testing ground for exploring
	// raw PCO data while building out the go-pco SDK, so the whole group is
	// gated behind APP_ENV=local rather than being real, public endpoints.
	group := router.Group("/pco", middleware.DevOnly())

	group.Get("/people", pc.People)
	group.Get("/plans", pc.Plans)
	group.Get("/songs", pc.Songs)
}

// People returns PCO people matching the given filters. All params are
// optional query string params:
//
//	first_name, last_name, email, phone - where[] filters
//	include                             - comma-separated related resources,
//	                                       e.g. "addresses,emails,phone_numbers"
//	order                               - a can_order_by field, e.g. "last_name"
//	                                       or "-created_at" for descending
//	per_page, offset                    - pagination
//
// Any resources pulled in via include= land in the response's top-level
// "included" array (JSON:API style) alongside "data" - nothing further to
// pass through, it's already part of the PersonListResponse that gets
// JSON-encoded below.
func (pc *PCOController) People(c *fiber.Ctx) error {
	params := &pco.PeopleParams{
		FirstName:         c.Query("first_name"),
		LastName:          c.Query("last_name"),
		Email:             c.Query("email"),
		SearchPhoneNumber: c.Query("phone"),
		OrderBy:           c.Query("order"),
		PerPage:           c.QueryInt("per_page"),
		Offset:            c.QueryInt("offset"),
	}

	if include := c.Query("include"); include != "" {
		params.Include = strings.Split(include, ",")
	}

	people, err := pco.GetPeople(middleware.RequestContext(c), params)
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(people)
}

// Plans returns PCO plans. PCO nests plans under a service type, and has no
// endpoint to list them all at once, so this either:
//   - fetches plans for a single service type, when ?service_type_id= is set
//   - otherwise fetches every service type, then every plan within each, and
//     returns them grouped by service type
//
// per_page/offset (applied to each plans request) are also accepted.
func (pc *PCOController) Plans(c *fiber.Ctx) error {
	plansParams := &pco.PlansParams{
		PerPage: c.QueryInt("per_page"),
		Offset:  c.QueryInt("offset"),
	}

	ctx := middleware.RequestContext(c)

	if serviceTypeID := c.Query("service_type_id"); serviceTypeID != "" {
		plans, err := pco.GetPlans(ctx, serviceTypeID, plansParams)
		if err != nil {
			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": err.Error()})
		}

		return c.JSON(plans)
	}

	serviceTypes, err := pco.GetServiceTypes(ctx, nil)
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": err.Error()})
	}

	type serviceTypePlans struct {
		ServiceType pco.ServiceTypeData `json:"service_type"`
		Plans       []pco.PlanData      `json:"plans"`
	}

	results := make([]serviceTypePlans, 0, len(serviceTypes.Data))
	var errs []string

	for _, serviceType := range serviceTypes.Data {
		plans, err := pco.GetPlans(ctx, serviceType.ID, plansParams)
		if err != nil {
			errs = append(errs, serviceType.ID+": "+err.Error())
			continue
		}

		results = append(results, serviceTypePlans{ServiceType: serviceType, Plans: plans.Data})
	}

	return c.JSON(fiber.Map{
		"service_types": results,
		"errors":        errs,
	})
}

// Songs returns songs from the organization's library. Optional query
// string params:
//
//	order             - a can_order_by field: "title", "created_at",
//	                     "updated_at", or "last_scheduled_at"; prefix "-" to
//	                     descend
//	per_page, offset  - pagination
func (pc *PCOController) Songs(c *fiber.Ctx) error {
	songs, err := pco.GetSongs(middleware.RequestContext(c), &pco.SongsParams{
		OrderBy: c.Query("order"),
		PerPage: c.QueryInt("per_page"),
		Offset:  c.QueryInt("offset"),
	})
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(songs)
}
