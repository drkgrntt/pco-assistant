package controllers

// Post-service debrief log / transition notes
// (docs/plans/shipped/transition-notes-plan.md): per-song or
// per-song-transition notes that resurface automatically when that song or
// pairing comes up again in a new draft set. Store methods live in
// internal/scheduling/song_transition_notes.go; this file is the
// controller-side matching logic (mirroring keyTransitionWarnings,
// sets_builder.go) plus the create/delete handlers backing the song
// detail card's own notes subsection (song_detail_card.templ).

import (
	"database/sql"
	"errors"
	"fmt"

	"pco-assistant/internal/scheduling"
	"pco-assistant/internal/server/middleware"
	views "pco-assistant/internal/views/scheduling"

	"github.com/gofiber/fiber/v2"
)

// transitionNoteKindLabel resolves a stored kind code to its display label
// - the one place either wording is spelled out, so songBadgeSpans/
// song_detail_card.templ don't each hardcode it separately.
func transitionNoteKindLabel(kind string) string {
	if kind == scheduling.TransitionNoteTalkingPoint {
		return "Talking point"
	}
	return "Debrief"
}

// transitionNoteKey identifies one (kind, songIDA, songIDB) lookup slot -
// shared by latestTransitionNotesByKey (building the map) and
// transitionNotePrompts (reading it), so the two can never drift out of
// sync on how a key is formed. songIDB "" is the single-song case.
func transitionNoteKey(kind, songIDA, songIDB string) string {
	return kind + "|" + songIDA + "|" + songIDB
}

// latestTransitionNotesByKey reduces every one of this org's transition
// notes down to the single latest row per (kind, songIDA, songIDB) key -
// the "read as latest row only" behavior docs/plans/shipped/
// transition-notes-plan.md's Schema section describes for talking points
// (debrief's own full log is transitionNotesBySong below, not this).
// notes must already be ordered newest-first (as ListTransitionNotesForSong
// returns them) - the first entry seen for a given key wins; every later
// one for that same key is a superseded duplicate and is skipped.
func latestTransitionNotesByKey(notes []scheduling.TransitionNote) map[string]scheduling.TransitionNote {
	latest := make(map[string]scheduling.TransitionNote, len(notes))
	for _, n := range notes {
		key := transitionNoteKey(n.Kind, n.SongIDA, n.SongIDB)
		if _, ok := latest[key]; !ok {
			latest[key] = n
		}
	}
	return latest
}

// transitionNotesBySong groups every one of this org's transition notes by
// every song id it touches - either side of a pair, or the sole song of a
// single-song note - so the song detail card can list "every note on file
// for this song" (see ListTransitionNotesForSong's "either side" query,
// mirroring ListSongRelationships) without a query per song. A note
// entered against a pair appears under *both* songs' own lists - see
// docs/plans/shipped/transition-notes-plan.md's resolved "both cards"
// decision. Notes stay in the same newest-first order within each song's
// bucket, since the input is already ordered that way.
func transitionNotesBySong(notes []scheduling.TransitionNote) map[string][]scheduling.TransitionNote {
	bySong := make(map[string][]scheduling.TransitionNote, len(notes))
	for _, n := range notes {
		bySong[n.SongIDA] = append(bySong[n.SongIDA], n)
		if n.SongIDB != "" {
			bySong[n.SongIDB] = append(bySong[n.SongIDB], n)
		}
	}
	return bySong
}

// buildTransitionNoteViews converts one song's full transition-note
// history (already newest-first, via transitionNotesBySong) into display
// view models for that song's own detail-card subsection.
// Direction/OtherTitle are both "" for a single-song note; otherwise
// Direction is "into" (songID was entered as song_id_a - this song
// transitions into OtherTitle) or "from" (songID was song_id_b - this
// song is the destination of a transition entered from OtherTitle).
func buildTransitionNoteViews(notes []scheduling.TransitionNote, songID string, titles map[string]string) []views.TransitionNoteEntryView {
	out := make([]views.TransitionNoteEntryView, 0, len(notes))
	for _, n := range notes {
		v := views.TransitionNoteEntryView{
			ID:        n.ID,
			Kind:      n.Kind,
			KindLabel: transitionNoteKindLabel(n.Kind),
			Note:      n.Note,
			CreatedAt: n.CreatedAt.Format("Jan 2, 2006"),
		}
		switch {
		case n.SongIDB == "":
			// Single-song note - no other side to name.
		case n.SongIDA == songID:
			v.Direction = "into"
			v.OtherTitle = titleOrID(titles, n.SongIDB)
		default:
			v.Direction = "from"
			v.OtherTitle = titleOrID(titles, n.SongIDA)
		}
		out = append(out, v)
	}
	return out
}

// transitionNotePrompt is one resurfacing prompt attached to a song row -
// the controller-side pair of views.TransitionNotePromptView, keyed by
// which row it renders on. Mirrors keyTransitionWarning's own shape
// (SongRowID + Label/Message) exactly.
type transitionNotePrompt struct {
	SongRowID int64
	Kind      string
	Label     string
	Message   string
}

// transitionNotePrompts walks rows the same way keyTransitionWarnings
// does, surfacing the latest debrief/talking-point note for:
//
//   - a single song, as soon as it's anywhere in rows - a plain membership
//     check, no adjacency needed (the brainstorm's own framing for a
//     single-song note - see docs/plans/shipped/transition-notes-plan.md's
//     Resurfacing trigger section); and
//
//   - a directional pair, only once the two songs are truly back-to-back
//     in rows with nothing between them (rows[i-1]/rows[i] - exactly the
//     adjacency keyTransitionWarnings already computes), checked in the
//     exact order the note was entered against, never the reverse (see
//     that plan's Schema section on why song_id_a/song_id_b are never
//     canonicalized here, unlike song_relationships).
//
// Every prompt attaches to the row at index i - a single-song note
// attaches to that song's own row; a pair note attaches to the *second*
// (later) song's row, the same placement decision keyTransitionWarning
// documents for the same reason (that's the row a director is actually
// looking at when the transition happens). Only the latest row per
// key+kind is ever surfaced here (via latest) - the full log lives on the
// song's own detail card instead (see buildTransitionNoteViews).
func transitionNotePrompts(rows []mergedRow, latest map[string]scheduling.TransitionNote, titles map[string]string) []transitionNotePrompt {
	var prompts []transitionNotePrompt
	kinds := [2]string{scheduling.TransitionNoteDebrief, scheduling.TransitionNoteTalkingPoint}

	for i, row := range rows {
		if row.Song == nil {
			continue
		}
		cur := row.Song

		for _, kind := range kinds {
			if note, ok := latest[transitionNoteKey(kind, cur.SongID, "")]; ok {
				prompts = append(prompts, transitionNotePrompt{
					SongRowID: cur.ID,
					Kind:      kind,
					Label:     transitionNoteKindLabel(kind) + " on file",
					Message:   note.Note,
				})
			}
		}

		if i == 0 || rows[i-1].Song == nil {
			continue
		}
		prev := rows[i-1].Song

		for _, kind := range kinds {
			note, ok := latest[transitionNoteKey(kind, prev.SongID, cur.SongID)]
			if !ok {
				continue
			}
			prompts = append(prompts, transitionNotePrompt{
				SongRowID: cur.ID,
				Kind:      kind,
				Label:     transitionNoteKindLabel(kind) + " on file",
				Message: fmt.Sprintf(
					"From last time (%s into %s): %s",
					titleOrID(titles, prev.SongID), titleOrID(titles, cur.SongID), note.Note,
				),
			})
		}
	}

	return prompts
}

// QuickAddTransitionNote creates a new debrief/talking-point note - see
// scheduling.CreateTransitionNoteParams. Posted from a song's own detail
// card (song_detail_card.templ's transition-notes subsection); song_id is
// always the song whose card the form was submitted from, other_song_id
// is an optional second song (a plain <select> of data.AllSongs - no
// SongPickerField yet, see that plan's own "song-picker dependency" open
// question), and direction says which side song_id occupies: "into" means
// song_id is song_id_a (this song transitions into other_song_id), "from"
// means song_id is song_id_b (this song is the destination of a
// transition entered from other_song_id). direction is ignored when
// other_song_id is "" (a single-song note has no direction to speak of).
//
// Same "quick add, re-render the whole body" shape as
// QuickAddSongRelationship/QuickAddSongTag (sets_detail.go) - a new note
// can change what's shown on up to two songs' own detail cards (see
// ListTransitionNotesForSong's "either side" listing) plus this song's own
// resurfacing prompt, so a full SetBuilderBody re-render (which includes
// songDetailCardsOOB) is simpler than hand-picking every fragment that
// could be affected.
//
// Deliberately NOT gated by data.ReadOnly anywhere in this handler - see
// song_detail_card.templ's own doc comment on the notes subsection for why
// a debrief specifically needs to stay writable against a past, read-only
// plan.
func (sc *SchedulingController) QuickAddTransitionNote(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")

	var body struct {
		SongID      string `form:"song_id"`
		OtherSongID string `form:"other_song_id"`
		Direction   string `form:"direction"`
		Kind        string `form:"kind"`
		Note        string `form:"note"`
	}
	errMsg := ""
	if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else {
		params := scheduling.CreateTransitionNoteParams{Kind: body.Kind, Note: body.Note}
		switch {
		case body.OtherSongID == "":
			params.SongIDA = body.SongID
		case body.Direction == "from":
			params.SongIDA = body.OtherSongID
			params.SongIDB = body.SongID
		default:
			params.SongIDA = body.SongID
			params.SongIDB = body.OtherSongID
		}
		if _, err := sc.store.CreateTransitionNote(c.Context(), orgID, params); err != nil {
			errMsg = err.Error()
		}
	}

	return render(c, views.SetBuilderBody(sc.buildSetBuilderDataWithErr(middleware.RequestContext(c), orgID, serviceTypeID, planID, errMsg)))
}

// DeleteTransitionNote removes a note - a plain input-correction
// affordance, mirroring DeleteSongRelationship/DeleteSongTag. Same "not
// gated by ReadOnly" reasoning as QuickAddTransitionNote above.
func (sc *SchedulingController) DeleteTransitionNote(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")

	errMsg := ""
	id, err := c.ParamsInt("id")
	if err != nil {
		errMsg = "id must be an integer"
	} else if delErr := sc.store.DeleteTransitionNote(c.Context(), orgID, int64(id)); delErr != nil && !errors.Is(delErr, sql.ErrNoRows) {
		errMsg = delErr.Error()
	}

	return render(c, views.SetBuilderBody(sc.buildSetBuilderDataWithErr(middleware.RequestContext(c), orgID, serviceTypeID, planID, errMsg)))
}
