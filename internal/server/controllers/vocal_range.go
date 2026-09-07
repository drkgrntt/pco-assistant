package controllers

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

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
// plus create/delete, no separate update) - a resubmit for the same person
// upserts in place (see scheduling.SetPersonVocalRange's ON CONFLICT
// clause), so there's no dedicated "edit" affordance the way relationships/
// preferences have (those can change *which* song/person/pair a row is
// about; a vocal range's only subject is the person themselves, so
// re-searching that same person and resubmitting is already the edit
// flow).

// vocalNoteRe matches a note-with-octave string like "C#4", "Bb2", "G3" - a
// note letter A-G, an optional single sharp/flat accidental (ASCII or
// Unicode), then a signed integer octave. This is standalone parsing, not
// an extension of parseKeyPitchClass (sets_builder.go): a vocal range's
// notes always carry an octave (a bare "G" doesn't say whether it's G3 or
// G5, which matters for a vocal range), and parseKeyPitchClass has no
// octave concept at all - see
// docs/plans/in-progress/vocal-range-plan.md's Phase 1 section.
var vocalNoteRe = regexp.MustCompile(`^([A-Ga-g])(#|♯|b|♭)?(-?[0-9]+)$`)

// minVocalOctave/maxVocalOctave bound what parseVocalNote accepts - wide
// enough for any real human vocal range (the lowest bass notes sit around
// E1/E2, the highest sopranos/countertenors reach around C6/C7) without
// accepting garbage like "C99".
const (
	minVocalOctave = 0
	maxVocalOctave = 8
)

// parseVocalNote parses a note-with-octave string ("C#4", "Bb2", "G3")
// into a 0-11 pitch class and an octave. It reuses keyPitchClasses
// (sets_builder.go) for the letter -> pitch class table parseKeyPitchClass
// also uses (a letter means the same pitch class either way - only the
// surrounding parse logic, an accidental followed by a required octave
// digit rather than nothing, is new). Returns ok=false for anything that
// isn't a note letter, an optional single accidental, then an integer
// octave within [minVocalOctave, maxVocalOctave].
func parseVocalNote(s string) (pitchClass, octave int, ok bool) {
	s = strings.TrimSpace(s)
	m := vocalNoteRe.FindStringSubmatch(s)
	if m == nil {
		return 0, 0, false
	}

	root, found := keyPitchClasses[strings.ToUpper(m[1])]
	if !found {
		return 0, 0, false
	}
	switch m[2] {
	case "#", "♯":
		root++
	case "b", "♭":
		root--
	}

	octave, err := strconv.Atoi(m[3])
	if err != nil || octave < minVocalOctave || octave > maxVocalOctave {
		return 0, 0, false
	}

	return ((root % 12) + 12) % 12, octave, true
}

// absoluteVocalSemitone combines a pitch class + octave into one
// internally-consistent absolute integer for comparison (octave*12 +
// pitch_class - no requirement to match real MIDI note numbering, just
// that two absolute values are directly comparable and subtractable), per
// docs/plans/in-progress/vocal-range-plan.md's storage-shape section.
func absoluteVocalSemitone(pitchClass, octave int) int {
	return octave*12 + pitchClass
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
