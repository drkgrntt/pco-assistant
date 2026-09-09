package controllers

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"

	"pco-assistant/internal/scheduling"
	"pco-assistant/internal/server/middleware"
	views "pco-assistant/internal/views/scheduling"

	"github.com/gofiber/fiber/v2"
)

// --- Person vocal range ------------------------------------------------
//
// Phase 1 of docs/plans/in-progress/vocal-range-plan.md: a standalone
// person-level vocal range setup page, mirroring PersonRelationshipsPage/
// CreatePersonRelationship/DeletePersonRelationship's shape (a page handler
// plus create/delete, no separate update route) - a resubmit for the same
// person upserts in place (see scheduling.SetPersonVocalRange's ON
// CONFLICT clause), so the Edit button (PersonVocalRangesTable,
// vocal_range.templ) just re-populates the existing form via editRow
// rather than needing its own update endpoint or a hidden "id" field:
// re-searching that same person and resubmitting genuinely is the same
// request an edit would make.

// parseVocalNote parses a note-with-octave string ("C#4", "Bb2", "G3")
// into a 0-11 pitch class and an octave - a thin wrapper around
// scheduling.ParseNote. This used to carry its own standalone regex/letter
// table (predating the key engine, docs/plans/in-progress/
// vocal-range-plan.md's Phase 2 prerequisite); now that
// internal/scheduling/key_engine.go exists as the single place this
// parsing logic lives, this just delegates rather than keeping a second
// copy - see that file's own ParseNote doc comment for the actual parsing
// rules (letter, optional single accidental, required octave digit,
// [scheduling.MinNoteOctave, scheduling.MaxNoteOctave]).
func parseVocalNote(s string) (pitchClass, octave int, ok bool) {
	return scheduling.ParseNote(s)
}

// absoluteVocalSemitone combines a pitch class + octave into one
// internally-consistent absolute integer for comparison - a thin wrapper
// around scheduling.AbsoluteSemitone, kept under this name so call sites
// in this file don't need to change; see that function's own doc comment
// for the storage-shape reasoning.
func absoluteVocalSemitone(pitchClass, octave int) int {
	return scheduling.AbsoluteSemitone(pitchClass, octave)
}

// vocalNoteNames is pitch class -> display name, sharp spelling only - a
// stored range has one canonical display form, unlike musicalKeyOptions'
// datalist which offers both sharp and flat spellings as separate pickable
// options.
var vocalNoteNames = [12]string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}

// formatVocalNote renders a stored pitch class/octave pair back into
// display text (e.g. pitch class 4, octave 4 -> "E4") - the reverse of
// parseVocalNote, for PersonVocalRangesTable.
func formatVocalNote(pitchClass, octave int) string {
	return fmt.Sprintf("%s%d", vocalNoteNames[((pitchClass%12)+12)%12], octave)
}

// personVocalRangeView formats personID's stored comfortable range (ranges,
// scheduling.ListPersonVocalRanges) for AssignmentView.VocalRangeDisplay and
// splits it into playable pitch-class/octave ints for the sidebar person
// detail card's tap-to-hear buttons (personDetailCard, mirroring
// songVocalRangeLine's own play buttons - see AssignmentView's doc comment
// for why this is always the as-recorded pair, no key transposition).
// hasRange is false when personID has no PersonVocalRange on file at all,
// in which case the caller leaves AssignmentView's vocal-range fields at
// their zero value and the card shows nothing, same "just omit it"
// convention as every other optional detail-card field.
func personVocalRangeView(ranges map[string]scheduling.PersonVocalRange, personID string) (display string, lowPC, lowOct, highPC, highOct int, hasRange bool) {
	vr, ok := ranges[personID]
	if !ok {
		return "", 0, 0, 0, 0, false
	}
	display = fmt.Sprintf("%s-%s", formatVocalNote(vr.LowestPitchClass, vr.LowestOctave), formatVocalNote(vr.HighestPitchClass, vr.HighestOctave))
	return display, vr.LowestPitchClass, vr.LowestOctave, vr.HighestPitchClass, vr.HighestOctave, true
}

func (sc *SchedulingController) PersonVocalRangePage(c *fiber.Ctx) error {
	_, personName := middleware.CurrentPerson(c)
	orgName := middleware.CurrentOrganization(c)
	orgID := middleware.CurrentOrganizationID(c)

	rangeMap, err := sc.store.ListPersonVocalRanges(c.Context(), orgID)
	if err != nil {
		return render(c, views.PersonVocalRanges(personName, orgName, nil, nil, err.Error()))
	}
	ranges := sortedVocalRanges(rangeMap)

	names := personNameMapForIDs(middleware.RequestContext(c), vocalRangePersonIDs(ranges))

	return render(c, views.PersonVocalRanges(personName, orgName, ranges, names, ""))
}

// sortedVocalRanges converts ListPersonVocalRanges' bulk map (keyed for O(1)
// lookup) into a stable-ordered slice for table rendering - newest first,
// like ListPersonRelationships/ListSongPersonPreferences already order by
// created_at DESC in SQL; a Go map has no iteration order at all, so this
// has to be sorted here rather than relying on range order.
func sortedVocalRanges(ranges map[string]scheduling.PersonVocalRange) []scheduling.PersonVocalRange {
	sorted := make([]scheduling.PersonVocalRange, 0, len(ranges))
	for _, r := range ranges {
		sorted = append(sorted, r)
	}
	sort.Slice(sorted, func(i, j int) bool {
		if !sorted[i].CreatedAt.Equal(sorted[j].CreatedAt) {
			return sorted[i].CreatedAt.After(sorted[j].CreatedAt)
		}
		return sorted[i].PersonID < sorted[j].PersonID
	})
	return sorted
}

// vocalRangePersonIDs collects a ranges slice's person ids, for a single
// batch of name lookups rather than one per row (see relationshipPersonIDs).
func vocalRangePersonIDs(ranges []scheduling.PersonVocalRange) []string {
	ids := make([]string, len(ranges))
	for i, r := range ranges {
		ids[i] = r.PersonID
	}
	return ids
}

func (sc *SchedulingController) SetPersonVocalRange(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)

	var body struct {
		PersonID string `form:"person_id"`
		Lowest   string `form:"lowest_note"`
		Highest  string `form:"highest_note"`
	}
	errMsg := ""
	if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else if body.PersonID == "" {
		errMsg = "person is required"
	} else if lowPC, lowOct, ok := parseVocalNote(body.Lowest); !ok {
		errMsg = fmt.Sprintf("%q isn't a note - try something like G3", body.Lowest)
	} else if highPC, highOct, ok := parseVocalNote(body.Highest); !ok {
		errMsg = fmt.Sprintf("%q isn't a note - try something like D5", body.Highest)
	} else if absoluteVocalSemitone(lowPC, lowOct) > absoluteVocalSemitone(highPC, highOct) {
		errMsg = "lowest note must be at or below the highest note"
	} else if _, err := sc.store.SetPersonVocalRange(c.Context(), orgID, scheduling.SetPersonVocalRangeParams{
		PersonID:          body.PersonID,
		LowestPitchClass:  lowPC,
		LowestOctave:      lowOct,
		HighestPitchClass: highPC,
		HighestOctave:     highOct,
	}); err != nil {
		errMsg = err.Error()
	}

	rangeMap, err := sc.store.ListPersonVocalRanges(c.Context(), orgID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}
	ranges := sortedVocalRanges(rangeMap)

	names := personNameMapForIDs(middleware.RequestContext(c), vocalRangePersonIDs(ranges))

	return render(c, views.PersonVocalRangesTable(ranges, names, errMsg))
}

func (sc *SchedulingController) DeletePersonVocalRange(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)

	id, err := c.ParamsInt("id")
	errMsg := ""
	if err != nil {
		errMsg = "id must be an integer"
	} else if delErr := sc.store.DeletePersonVocalRange(c.Context(), orgID, int64(id)); delErr != nil && !errors.Is(delErr, sql.ErrNoRows) {
		errMsg = delErr.Error()
	}

	rangeMap, err := sc.store.ListPersonVocalRanges(c.Context(), orgID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}
	ranges := sortedVocalRanges(rangeMap)

	names := personNameMapForIDs(middleware.RequestContext(c), vocalRangePersonIDs(ranges))

	return render(c, views.PersonVocalRangesTable(ranges, names, errMsg))
}
