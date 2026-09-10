package controllers

// Per-song set builder mutations: add/remove/duplicate/replace, plus the
// description/key/note edit forms and the quick-add panel.

import (
	"context"
	"database/sql"
	"errors"
	"unicode"

	"pco-assistant/internal/scheduling"
	"pco-assistant/internal/server/middleware"
	views "pco-assistant/internal/views/scheduling"

	pco "github.com/drkgrntt/go-pco"
	"github.com/gofiber/fiber/v2"
)

func (sc *SchedulingController) AddSetSong(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")

	var body struct {
		SongID string `form:"song_id"`
	}
	errMsg := ""
	chainedAfterSongID := ""
	if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else if body.SongID == "" {
		errMsg = "song_id is required"
	} else if set, err := sc.store.GetOrCreateSet(c.Context(), orgID, serviceTypeID, planID); err != nil {
		errMsg = err.Error()
	} else if _, chained, err := sc.store.AddSongToSet(c.Context(), set.ID, body.SongID, sc.firstSongFallbackAnchor(middleware.RequestContext(c), orgID, serviceTypeID, planID, set.ID)); err != nil {
		errMsg = err.Error()
	} else {
		chainedAfterSongID = chained
	}

	data := sc.buildSetBuilderDataWithErr(middleware.RequestContext(c), orgID, serviceTypeID, planID, errMsg)

	// SongPairPrompt: only ever set here, right after a successful add that
	// actually chained after another song - see AddSongToSet's doc comment
	// and views.SongPairPromptView. Every other mutation (remove/duplicate/
	// replace/reposition/publish/revert) leaves it nil, so the prompt
	// naturally disappears again the moment you do anything else, without
	// needing an explicit dismiss/expiry mechanism. Titles are read off the
	// set data just built rather than re-resolved - both songs are
	// necessarily still in data.SetSongs at this point.
	if chainedAfterSongID != "" {
		prompt := &views.SongPairPromptView{PreviousSongID: chainedAfterSongID, NewSongID: body.SongID}
		for _, ss := range data.SetSongs {
			if ss.SongID == chainedAfterSongID {
				prompt.PreviousSongTitle = ss.Title
			}
			if ss.SongID == body.SongID {
				prompt.NewSongTitle = ss.Title
			}
		}
		if prompt.PreviousSongTitle != "" && prompt.NewSongTitle != "" {
			data.SongPairPrompt = prompt
		}
	}

	return render(c, views.SetBuilderBody(data))
}

// firstSongFallbackAnchor resolves this service type's remembered "where
// the first song usually goes" (see PreferredFirstSongAnchorTitle,
// written by buildSetBuilderData on every render - see its own comment)
// against planID's actual current items, for AddSongToSet's
// fallbackAnchorItemID parameter - used only when there's nothing else to
// chain from (an empty set). Deliberately conservative: only ever applies
// when the set has literally no active songs yet, not merely no valid
// chaining pointer (e.g. right after Revert/Import, which nulls
// last_touched_set_song_id even though the set isn't empty - that case
// should keep AddSongToSet's own existing top-of-service fallback, not
// this one). Costs nothing beyond today's behavior unless the set really
// is empty and something's actually been remembered: the ListSetSongs
// call is cheap and was effectively free, and the PCO items fetch only
// happens when there's a real title to look for.
func (sc *SchedulingController) firstSongFallbackAnchor(ctx context.Context, organizationID, serviceTypeID, planID string, setID int64) string {
	existing, err := sc.store.ListSetSongs(ctx, setID)
	if err != nil || len(existing) > 0 {
		return ""
	}

	title, ok := sc.store.PreferredFirstSongAnchorTitle(ctx, organizationID, serviceTypeID)
	if !ok || title == "" {
		return ""
	}

	items, err := pco.GetItems(ctx, serviceTypeID, planID, nil)
	if err != nil {
		return ""
	}
	for _, item := range items.Data {
		label := item.Attributes.Title
		if label == "" {
			label = item.Attributes.ItemType
		}
		if label == title {
			return item.ID
		}
	}
	return ""
}

func (sc *SchedulingController) RemoveSetSong(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")

	errMsg := ""
	if rowID, err := c.ParamsInt("set_song_id"); err != nil {
		errMsg = "set_song_id must be an integer"
	} else if set, err := sc.store.GetOrCreateSet(c.Context(), orgID, serviceTypeID, planID); err != nil {
		errMsg = err.Error()
	} else if err := sc.store.RemoveSongFromSet(c.Context(), set.ID, int64(rowID)); err != nil && !errors.Is(err, sql.ErrNoRows) {
		errMsg = err.Error()
	}

	return render(c, views.SetBuilderBody(sc.buildSetBuilderDataWithErr(middleware.RequestContext(c), orgID, serviceTypeID, planID, errMsg)))
}

// DuplicateSetSong creates a second occurrence of an existing set song
// (same library song, own independent row/position/publish state) right
// after it - for a song that's reprised elsewhere in the service, e.g. the
// same opener sung again as the response.
func (sc *SchedulingController) DuplicateSetSong(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")

	errMsg := ""
	if rowID, err := c.ParamsInt("set_song_id"); err != nil {
		errMsg = "set_song_id must be an integer"
	} else if set, err := sc.store.GetOrCreateSet(c.Context(), orgID, serviceTypeID, planID); err != nil {
		errMsg = err.Error()
	} else if _, err := sc.store.DuplicateSongInSet(c.Context(), set.ID, int64(rowID)); err != nil {
		errMsg = err.Error()
	}

	return render(c, views.SetBuilderBody(sc.buildSetBuilderDataWithErr(middleware.RequestContext(c), orgID, serviceTypeID, planID, errMsg)))
}

// findSongViewByRowID/findSongViewByItemID locate one song's already-built
// view inside data.ServiceOrder - used by UpdateSetSongDescription/
// UpdateSongNote to re-render just the one form that was actually
// submitted (see views.DescriptionFormFragment/SongNoteFormFragment)
// instead of the whole set builder body, since neither action touches
// anything else on the page - not even the rest of the same card.
func findSongViewByRowID(data views.SetBuilderData, rowID int64) (views.SetSongView, bool) {
	for _, row := range data.ServiceOrder {
		if row.IsSong && row.Song.ID == rowID {
			return row.Song, true
		}
	}
	return views.SetSongView{}, false
}

func findSongViewByItemID(data views.SetBuilderData, itemID string) (views.SetSongView, bool) {
	for _, row := range data.ServiceOrder {
		if row.IsSong && row.Song.PCOItemID == itemID {
			return row.Song, true
		}
	}
	return views.SetSongView{}, false
}

// UpdateSetSongDescription sets one set song's local per-occurrence note
// (see scheduling.SetSong.Description) - a plain local draft edit, not a
// live Planning Center write. It reaches Planning Center as that item's
// Description on the next Publish or Refresh song info, same as every
// other local change on this page (see itemDescriptionFor, scheduling.go).
//
// Re-renders just this one form (see views.DescriptionFormFragment's own
// doc comment for why) rather than the whole SetBuilderBody, or even the
// whole card - a description edit never changes the order of service,
// publish status, this card's own title/badges, or any other note, so
// there's nothing else that needs to reflect it.
func (sc *SchedulingController) UpdateSetSongDescription(c *fiber.Ctx) error {
	ctx := middleware.RequestContext(c)
	orgID := middleware.CurrentOrganizationID(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")

	rowID, err := c.ParamsInt("set_song_id")
	if err != nil {
		return render(c, views.SetBuilderBody(sc.buildSetBuilderDataWithErr(ctx, orgID, serviceTypeID, planID, "set_song_id must be an integer")))
	}

	errMsg := ""
	if set, err := sc.store.GetOrCreateSet(ctx, orgID, serviceTypeID, planID); err != nil {
		errMsg = err.Error()
	} else if err := sc.store.UpdateSetSongDescription(ctx, set.ID, int64(rowID), c.FormValue("description")); err != nil {
		errMsg = err.Error()
	}

	data := sc.buildSetBuilderData(ctx, orgID, serviceTypeID, planID)
	song, ok := findSongViewByRowID(data, int64(rowID))
	if !ok {
		// The row's gone (e.g. removed in another tab since this form was
		// last rendered) - nothing left to render a fragment for, fall back
		// to a full re-render so the page still reflects reality.
		data.ErrMsg = appendErr(errMsg, data.ErrMsg)
		return render(c, views.SetBuilderBody(data))
	}
	return render(c, views.DescriptionFormFragment(data, song, errMsg))
}

// UpdateSetSongKey sets one set song's local per-occurrence key override
// (see scheduling.SetSong.KeyOverride) - same local-draft-only semantics
// as UpdateSetSongDescription above (it reaches Planning Center as a Key
// relationship on the next Publish or Refresh song info, see
// resolveKeyID, not immediately), and the same single-form fragment
// response.
func (sc *SchedulingController) UpdateSetSongKey(c *fiber.Ctx) error {
	ctx := middleware.RequestContext(c)
	orgID := middleware.CurrentOrganizationID(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")

	rowID, err := c.ParamsInt("set_song_id")
	if err != nil {
		return render(c, views.SetBuilderBody(sc.buildSetBuilderDataWithErr(ctx, orgID, serviceTypeID, planID, "set_song_id must be an integer")))
	}

	errMsg := ""
	if set, err := sc.store.GetOrCreateSet(ctx, orgID, serviceTypeID, planID); err != nil {
		errMsg = err.Error()
	} else if err := sc.store.UpdateSetSongKeyOverride(ctx, set.ID, int64(rowID), uppercaseFirst(c.FormValue("key"))); err != nil {
		errMsg = err.Error()
	}

	data := sc.buildSetBuilderData(ctx, orgID, serviceTypeID, planID)
	song, ok := findSongViewByRowID(data, int64(rowID))
	if !ok {
		data.ErrMsg = appendErr(errMsg, data.ErrMsg)
		return render(c, views.SetBuilderBody(data))
	}
	return render(c, views.KeyFormFragment(data, song, errMsg))
}

// UpdateSetSongLeader sets one set song's local per-occurrence song leader
// (see scheduling.SetSong.LeaderPersonID) - same local-draft-only
// semantics as UpdateSetSongDescription/UpdateSetSongKey above (it reaches
// Planning Center as a real ItemAssignment on the next Publish or Refresh
// song info, see syncLeaderAssignment in sets_publish.go/leader_note.go,
// not immediately), and the same single-form fragment response.
func (sc *SchedulingController) UpdateSetSongLeader(c *fiber.Ctx) error {
	ctx := middleware.RequestContext(c)
	orgID := middleware.CurrentOrganizationID(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")

	rowID, err := c.ParamsInt("set_song_id")
	if err != nil {
		return render(c, views.SetBuilderBody(sc.buildSetBuilderDataWithErr(ctx, orgID, serviceTypeID, planID, "set_song_id must be an integer")))
	}

	errMsg := ""
	if set, err := sc.store.GetOrCreateSet(ctx, orgID, serviceTypeID, planID); err != nil {
		errMsg = err.Error()
	} else if err := sc.store.UpdateSetSongLeader(ctx, set.ID, int64(rowID), c.FormValue("leader_person_id")); err != nil {
		errMsg = err.Error()
	}

	data := sc.buildSetBuilderData(ctx, orgID, serviceTypeID, planID)
	song, ok := findSongViewByRowID(data, int64(rowID))
	if !ok {
		data.ErrMsg = appendErr(errMsg, data.ErrMsg)
		return render(c, views.SetBuilderBody(data))
	}
	return render(c, views.LeaderFormFragment(data, song, errMsg))
}

// uppercaseFirst uppercases just the key's first letter ("bb" -> "Bb", "f#m"
// -> "F#m") - normalization only, not validation. The rest of the string is
// left untouched (and PCO's own key strings aren't a fixed enum this app
// hardcodes against - see keyForm's doc comment), so this never rejects
// anything, it just fixes the most common typo of typing a lowercase note
// letter.
func uppercaseFirst(key string) string {
	runes := []rune(key)
	if len(runes) == 0 {
		return key
	}
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

// UpdateSongNote creates, updates, or deletes one category's Planning
// Center ItemNote on a published song's plan item - unlike
// UpdateSetSongDescription above, this writes straight to Planning Center
// immediately, the same "ad hoc setup-level write" shape as
// QuickAddSongTag/QuickAddSongRelationship/QuickAddSongRole, just against
// a real PCO record instead of this app's own tables. note_id present
// means update that note (or delete it, if content was cleared to empty -
// tidier than leaving an empty note sitting in Planning Center); note_id
// empty and content non-empty means create a new one for category_id;
// note_id empty and content empty is a no-op (nothing existed, nothing was
// typed).
//
// Re-renders just this one category's own form - same reasoning as
// UpdateSetSongDescription above (a note write here never touches
// anything else on the page, including any of this song's other notes).
func (sc *SchedulingController) UpdateSongNote(c *fiber.Ctx) error {
	ctx := middleware.RequestContext(c)
	orgID := middleware.CurrentOrganizationID(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")
	itemID := c.Params("item_id")
	categoryID := c.FormValue("category_id")
	noteID := c.FormValue("note_id")
	content := c.FormValue("content")

	errMsg := ""
	switch {
	case content == "" && noteID != "":
		if err := pco.DeleteItemNote(ctx, serviceTypeID, planID, itemID, noteID); err != nil {
			errMsg = "removing note from Planning Center: " + err.Error()
		}
	case content == "":
		// Nothing to do.
	case noteID != "":
		if _, err := pco.UpdateItemNote(ctx, serviceTypeID, planID, itemID, noteID, &pco.UpdateItemNoteParams{Content: content}); err != nil {
			errMsg = "updating note in Planning Center: " + err.Error()
		}
	default:
		if _, err := pco.CreateItemNote(ctx, serviceTypeID, planID, itemID, &pco.CreateItemNoteParams{Content: content, ItemNoteCategoryID: categoryID}); err != nil {
			errMsg = "saving note to Planning Center: " + err.Error()
		}
	}

	data := sc.buildSetBuilderData(ctx, orgID, serviceTypeID, planID)
	song, ok := findSongViewByItemID(data, itemID)
	if !ok {
		data.ErrMsg = appendErr(errMsg, data.ErrMsg)
		return render(c, views.SetBuilderBody(data))
	}
	note, ok := findSongNoteByCategoryID(song, categoryID)
	if !ok {
		// The category's gone from the fresh fetch above (e.g. deleted in
		// Planning Center between page load and this save) - same
		// fall-back reasoning as the row-gone case above.
		data.ErrMsg = appendErr(errMsg, data.ErrMsg)
		return render(c, views.SetBuilderBody(data))
	}
	return render(c, views.SongNoteFormFragment(data, song, note, errMsg))
}

func findSongNoteByCategoryID(song views.SetSongView, categoryID string) (views.SongNoteView, bool) {
	for _, note := range song.Notes {
		if note.CategoryID == categoryID {
			return note, true
		}
	}
	return views.SongNoteView{}, false
}

// ReplaceSetSong swaps the set song identified in the URL for the posted
// song_id, in place - see scheduling.Store.ReplaceSongInSet.
func (sc *SchedulingController) ReplaceSetSong(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")

	var body struct {
		SongID string `form:"song_id"`
	}
	errMsg := ""
	rowID, rowErr := c.ParamsInt("set_song_id")
	if rowErr != nil {
		errMsg = "set_song_id must be an integer"
	} else if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else if body.SongID == "" {
		errMsg = "song_id is required"
	} else if set, err := sc.store.GetOrCreateSet(c.Context(), orgID, serviceTypeID, planID); err != nil {
		errMsg = err.Error()
	} else if _, err := sc.store.ReplaceSongInSet(c.Context(), set.ID, int64(rowID), body.SongID); err != nil {
		errMsg = err.Error()
	}

	return render(c, views.SetBuilderBody(sc.buildSetBuilderDataWithErr(middleware.RequestContext(c), orgID, serviceTypeID, planID, errMsg)))
}

// QuickAddSongTag/QuickAddSongRelationship/QuickAddSongRole/
// QuickAddSongContext/QuickAddSongVocalRange back the set builder's "Quick
// add" panel (set_builder.templ) - the same setup-level entities as
// SongTagsPage/RelationshipsPage/SongRolesPage/SongContextsPage/
// SongVocalRangePage's own create forms (same Store methods, same
// params/parsing), just posted from the build page and re-rendering
// SetBuilderBody instead of a setup-page table, so e.g. a newly-tagged
// song's badge shows up immediately on its candidate row without leaving
// this page. Deliberately create-only - see the build-page redesign plan
// for why editing these stays the dedicated setup pages' job.

func (sc *SchedulingController) QuickAddSongTag(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")

	var body scheduling.CreateSongTagParams
	errMsg := ""
	if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else if _, err := sc.store.CreateSongTag(c.Context(), orgID, body); err != nil {
		errMsg = err.Error()
	}

	return render(c, views.SetBuilderBody(sc.buildSetBuilderDataWithErr(middleware.RequestContext(c), orgID, serviceTypeID, planID, errMsg)))
}

func (sc *SchedulingController) QuickAddSongRelationship(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")

	var body scheduling.CreateSongRelationshipParams
	errMsg := ""
	if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else if _, err := sc.store.CreateSongRelationship(c.Context(), orgID, body); err != nil {
		errMsg = err.Error()
	}

	return render(c, views.SetBuilderBody(sc.buildSetBuilderDataWithErr(middleware.RequestContext(c), orgID, serviceTypeID, planID, errMsg)))
}

func (sc *SchedulingController) QuickAddSongRole(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")

	var body scheduling.CreateSongRoleParams
	errMsg := ""
	if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else if _, err := sc.store.CreateSongRole(c.Context(), orgID, body); err != nil {
		errMsg = err.Error()
	}

	return render(c, views.SetBuilderBody(sc.buildSetBuilderDataWithErr(middleware.RequestContext(c), orgID, serviceTypeID, planID, errMsg)))
}

// QuickAddSongContext is QuickAddSongRole's exact counterpart for song
// contexts (see song_contexts.go) - same shape, same
// setup-level-entity-posted-from-the-build-page reasoning as
// QuickAddSongTag/QuickAddSongRelationship/QuickAddSongRole above.
func (sc *SchedulingController) QuickAddSongContext(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")

	var body scheduling.CreateSongContextParams
	errMsg := ""
	if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else if _, err := sc.store.CreateSongContext(c.Context(), orgID, body); err != nil {
		errMsg = err.Error()
	}

	return render(c, views.SetBuilderBody(sc.buildSetBuilderDataWithErr(middleware.RequestContext(c), orgID, serviceTypeID, planID, errMsg)))
}

// QuickAddSongVocalRange is Part C of docs/plans/in-progress/
// vocal-range-plan.md - the Build page's quick-add-panel/row-kebab-menu
// entry point for scheduling.Store's SetSongVocalRange, the same Store
// method (via setSongVocalRangeFromForm, song_vocal_range.go) the
// dedicated /scheduling/song-vocal-range setup page's own form already
// uses - see that helper's own doc comment for why the validation lives
// there instead of being duplicated here. No explicit
// triggerSongInfoQueueSync call here (unlike that setup page's own
// handlers) - buildSetBuilderDataWithErr below already defers one for
// every render it backs, same as QuickAddSongTag/QuickAddSongRelationship/
// QuickAddSongRole above.
func (sc *SchedulingController) QuickAddSongVocalRange(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")
	ctx := middleware.RequestContext(c)

	var body struct {
		SongID       string `form:"song_id"`
		ReferenceKey string `form:"reference_key"`
		Lowest       string `form:"lowest_note"`
		Highest      string `form:"highest_note"`
	}
	errMsg := ""
	if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else {
		errMsg = sc.setSongVocalRangeFromForm(ctx, orgID, body.SongID, body.ReferenceKey, body.Lowest, body.Highest)
	}

	return render(c, views.SetBuilderBody(sc.buildSetBuilderDataWithErr(ctx, orgID, serviceTypeID, planID, errMsg)))
}
