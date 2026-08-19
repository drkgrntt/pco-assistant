package controllers

import (
	"database/sql"
	"errors"

	"pco-assistant/internal/scheduling"

	"github.com/gofiber/fiber/v2"
)

type SchedulingAPIController struct {
	store *scheduling.Store
}

func NewSchedulingAPIController(store *scheduling.Store) *SchedulingAPIController {
	return &SchedulingAPIController{store: store}
}

func (sc *SchedulingAPIController) RegisterRoutes(router fiber.Router) {
	group := router.Group("/api/scheduling")

	group.Post("/song-relationships", sc.CreateSongRelationship)
	group.Get("/song-relationships", sc.ListSongRelationships)
	group.Delete("/song-relationships/:id", sc.DeleteSongRelationship)

	group.Post("/song-preferences", sc.SetSongPersonPreference)
	group.Get("/song-preferences", sc.ListSongPersonPreferences)
	group.Delete("/song-preferences/:id", sc.DeleteSongPersonPreference)
}

func (sc *SchedulingAPIController) CreateSongRelationship(c *fiber.Ctx) error {
	var body scheduling.CreateSongRelationshipParams
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	rel, err := sc.store.CreateSongRelationship(c.Context(), body)
	if err != nil {
		return c.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(fiber.StatusCreated).JSON(rel)
}

// ListSongRelationships lists relationships. ?song_id= filters to
// relationships involving that song on either side; omitted, it lists all.
func (sc *SchedulingAPIController) ListSongRelationships(c *fiber.Ctx) error {
	relationships, err := sc.store.ListSongRelationships(c.Context(), c.Query("song_id"))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(relationships)
}

func (sc *SchedulingAPIController) DeleteSongRelationship(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "id must be an integer"})
	}

	if err := sc.store.DeleteSongRelationship(c.Context(), int64(id)); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return c.SendStatus(fiber.StatusNotFound)
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// SetSongPersonPreference creates or replaces the preference for a
// song/person pair.
func (sc *SchedulingAPIController) SetSongPersonPreference(c *fiber.Ctx) error {
	var body scheduling.SetSongPersonPreferenceParams
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	pref, err := sc.store.SetSongPersonPreference(c.Context(), body)
	if err != nil {
		return c.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(fiber.StatusOK).JSON(pref)
}

// ListSongPersonPreferences lists preferences, optionally filtered by
// ?song_id= and/or ?person_id=.
func (sc *SchedulingAPIController) ListSongPersonPreferences(c *fiber.Ctx) error {
	preferences, err := sc.store.ListSongPersonPreferences(c.Context(), c.Query("song_id"), c.Query("person_id"))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(preferences)
}

func (sc *SchedulingAPIController) DeleteSongPersonPreference(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "id must be an integer"})
	}

	if err := sc.store.DeleteSongPersonPreference(c.Context(), int64(id)); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return c.SendStatus(fiber.StatusNotFound)
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.SendStatus(fiber.StatusNoContent)
}
