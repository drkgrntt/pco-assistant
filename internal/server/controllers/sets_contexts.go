package controllers

// Song contexts ("where this song gets sung" - big church, youth, other
// campus, or whatever an org actually calls its own contexts) - setup page
// CRUD. Two halves: managing the org's own list of context types
// (song_context_types - this app's first per-org *editable* lookup, unlike
// song_role_types) and tagging songs with them (song_contexts). The type
// handlers (CreateSongContextType/DeleteSongContextType) stay a flat-table
// CRUD, same shape as sets_roles.go's own. The song-tagging handlers
// (CreateSongContext/BulkCreateSongContext/DeleteSongContext) instead
// mirror song_tags.go's card-per-song shape (see
// docs/plans/shipped/song-tags-setup-redesign-plan.md and
// song_contexts.templ's own doc comment) - one persistent card per active
// song plus a bulk "tag several songs with one context" form, not a flat
// table.

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"pco-assistant/internal/scheduling"
	"pco-assistant/internal/server/middleware"
	views "pco-assistant/internal/views/scheduling"

	"github.com/gofiber/fiber/v2"
)

// contextTypeLabelMap builds a context_type_id -> label lookup from
// ListSongContextTypes, for resolving a tagged song's stored context id
// back to its display label (see SongContextCard) - the int64-keyed
// counterpart of songRoleTypeLabelMap.
func contextTypeLabelMap(types []scheduling.SongContextType) map[int64]string {
	labels := make(map[int64]string, len(types))
	for _, t := range types {
		labels[t.ID] = t.Label
	}
	return labels
}

func (sc *SchedulingController) SongContextsPage(c *fiber.Ctx) error {
	_, personName := middleware.CurrentPerson(c)
	orgName := middleware.CurrentOrganization(c)
	orgID := middleware.CurrentOrganizationID(c)

	songs, songsErr := fetchActiveSongs(middleware.RequestContext(c), orgID)

	contextTypes, err := sc.store.ListSongContextTypes(c.Context(), orgID)
	if err != nil {
		return render(c, views.SongContexts(personName, orgName, songs, contextTypes, nil, nil, err.Error()))
	}

	contexts, err := sc.store.ListSongContext(c.Context(), orgID, "")
	if err != nil {
		return render(c, views.SongContexts(personName, orgName, songs, contextTypes, nil, contextTypeLabelMap(contextTypes), err.Error()))
	}

	errMsg := ""
	if songsErr != nil {
		errMsg = "loading songs from Planning Center: " + songsErr.Error()
	}

	return render(c, views.SongContexts(personName, orgName, songs, contextTypes, songContextsBySong(contexts), contextTypeLabelMap(contextTypes), errMsg))
}

// CreateSongContextType creates (or, via the shared parseFormID edit
// convention, renames) one of this org's own context types - re-renders
// just the types list fragment plus an out-of-band refresh of the
// bulk-add form's own context <select> (songContextSelectOOB), so a
// newly-created context is immediately pickable there without a page
// reload - see SongContextTypesMutationResponse's own doc comment (and
// songContextBulkAddForm's, for why only that one select stays in sync
// this way, not every per-song card's own select too).
func (sc *SchedulingController) CreateSongContextType(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)

	label := strings.TrimSpace(c.FormValue("label"))
	errMsg := ""
	if id, editing, idErr := parseFormID(c); idErr != nil {
		errMsg = "id must be an integer"
	} else if editing {
		if _, err := sc.store.UpdateSongContextType(c.Context(), orgID, id, label); err != nil {
			errMsg = err.Error()
		}
	} else if _, err := sc.store.CreateSongContextType(c.Context(), orgID, label); err != nil {
		errMsg = err.Error()
	}

	contextTypes, err := sc.store.ListSongContextTypes(c.Context(), orgID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}

	return render(c, views.SongContextTypesMutationResponse(contextTypes, errMsg))
}

// DeleteSongContextType removes one of this org's context types -
// song_contexts_type_id's ON DELETE CASCADE (migration 00045) also removes
// every song's assignment to it, which the setup page's delete button
// confirms explicitly before this ever fires (see song_contexts.templ).
func (sc *SchedulingController) DeleteSongContextType(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)

	id, err := c.ParamsInt("id")
	errMsg := ""
	if err != nil {
		errMsg = "id must be an integer"
	} else if delErr := sc.store.DeleteSongContextType(c.Context(), orgID, int64(id)); delErr != nil && !errors.Is(delErr, sql.ErrNoRows) {
		errMsg = delErr.Error()
	}

	contextTypes, listErr := sc.store.ListSongContextTypes(c.Context(), orgID)
	if listErr != nil {
		return c.Status(fiber.StatusInternalServerError).SendString(listErr.Error())
	}

	return render(c, views.SongContextTypesMutationResponse(contextTypes, errMsg))
}

func (sc *SchedulingController) CreateSongContext(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)

	var body scheduling.CreateSongContextParams
	errMsg := ""
	if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else if id, editing, idErr := parseFormID(c); idErr != nil {
		errMsg = "id must be an integer"
	} else if editing {
		if _, err := sc.store.UpdateSongContext(c.Context(), orgID, id, body); err != nil {
			errMsg = err.Error()
		}
	} else if _, err := sc.store.CreateSongContext(c.Context(), orgID, body); err != nil {
		errMsg = err.Error()
	}

	contexts, err := sc.store.ListSongContext(c.Context(), orgID, "")
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}

	songs, _ := fetchActiveSongs(middleware.RequestContext(c), orgID)
	contextTypes, _ := sc.store.ListSongContextTypes(c.Context(), orgID)

	return render(c, views.SongContextsMutationResponse(songs, songContextsBySong(contexts), contextTypes, contextTypeLabelMap(contextTypes), "", errMsg))
}

// BulkCreateSongContext applies one context type to every selected song at
// once - the counterpart to CreateSongContext's per-song inline form,
// mirroring BulkCreateSongTag's shape exactly (song_tags.go): loops
// body.SongIDs, calls store.CreateSongContext per song (its ON CONFLICT
// upsert makes a repeat selection of an already-tagged song a harmless
// no-op rather than an error), tolerates partial failure via appendErr,
// and builds a successMsg so a bulk add that touched songs scattered well
// below the fold still confirms it did something.
func (sc *SchedulingController) BulkCreateSongContext(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	ctx := middleware.RequestContext(c)

	var body struct {
		ContextTypeID int64    `form:"context_type_id"`
		SongIDs       []string `form:"song_ids"`
	}

	errMsg := ""
	applied := 0
	if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else if len(body.SongIDs) == 0 {
		errMsg = "select at least one song to tag with this context"
	} else {
		for _, songID := range body.SongIDs {
			if _, err := sc.store.CreateSongContext(ctx, orgID, scheduling.CreateSongContextParams{
				SongID: songID, ContextTypeID: body.ContextTypeID,
			}); err != nil {
				errMsg = appendErr(errMsg, err.Error())
				continue
			}
			applied++
		}
	}

	contexts, err := sc.store.ListSongContext(ctx, orgID, "")
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}

	songs, _ := fetchActiveSongs(ctx, orgID)
	contextTypes, _ := sc.store.ListSongContextTypes(ctx, orgID)
	contextLabels := contextTypeLabelMap(contextTypes)

	successMsg := ""
	if applied > 0 {
		label, ok := contextLabels[body.ContextTypeID]
		if !ok {
			label = strconv.FormatInt(body.ContextTypeID, 10)
		}
		successMsg = fmt.Sprintf("Tagged %d song(s) with %q.", applied, label)
	}

	return render(c, views.SongContextsMutationResponse(songs, songContextsBySong(contexts), contextTypes, contextLabels, successMsg, errMsg))
}

func (sc *SchedulingController) DeleteSongContext(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)

	id, err := c.ParamsInt("id")
	errMsg := ""
	if err != nil {
		errMsg = "id must be an integer"
	} else if delErr := sc.store.DeleteSongContext(c.Context(), orgID, int64(id)); delErr != nil && !errors.Is(delErr, sql.ErrNoRows) {
		errMsg = delErr.Error()
	}

	contexts, listErr := sc.store.ListSongContext(c.Context(), orgID, "")
	if listErr != nil {
		return c.Status(fiber.StatusInternalServerError).SendString(listErr.Error())
	}

	songs, _ := fetchActiveSongs(middleware.RequestContext(c), orgID)
	contextTypes, _ := sc.store.ListSongContextTypes(c.Context(), orgID)

	return render(c, views.SongContextsMutationResponse(songs, songContextsBySong(contexts), contextTypes, contextTypeLabelMap(contextTypes), "", errMsg))
}
