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

// --- Song vocal range ---------------------------------------------------
//
// Phase 2 of docs/plans/in-progress/vocal-range-plan.md: a standalone
// song-level vocal range setup page, mirroring song_tags.go's shape (a
// song <select>-driven page handler plus create/delete, no separate
// update - a resubmit for the same song upserts in place, see
// scheduling.SetSongVocalRange's ON CONFLICT clause). Deliberately NOT on
// the Build page's song detail card - a song's range is a fact about the
// song itself, reused across every plan it appears in, exactly like
// song_tags/song_roles, not a per-occurrence value - see that plan doc's
// own reasoning. The Build page only ever *reads* the transposed result
// (sets_builder.go's buildSetBuilderData/renderServiceOrder).

func (sc *SchedulingController) SongVocalRangePage(c *fiber.Ctx) error {
	_, personName := middleware.CurrentPerson(c)
	orgName := middleware.CurrentOrganization(c)
	orgID := middleware.CurrentOrganizationID(c)

	songs, songsErr := fetchActiveSongs(middleware.RequestContext(c), orgID)

	rangeMap, err := sc.store.ListSongVocalRanges(c.Context(), orgID)
	if err != nil {
		return render(c, views.SongVocalRanges(personName, orgName, songs, nil, songTitleMap(songs), err.Error()))
	}
	ranges := sortedSongVocalRanges(rangeMap)

	errMsg := ""
	if songsErr != nil {
		errMsg = "loading songs from Planning Center: " + songsErr.Error()
	}

	return render(c, views.SongVocalRanges(personName, orgName, songs, ranges, songTitleMap(songs), errMsg))
}

// sortedSongVocalRanges converts ListSongVocalRanges' bulk map (keyed for
// O(1) lookup) into a stable-ordered slice for table rendering - newest
// first, mirroring sortedVocalRanges (vocal_range.go); a Go map has no
// iteration order at all, so this has to be sorted here rather than
// relying on range order.
func sortedSongVocalRanges(ranges map[string]scheduling.SongVocalRange) []scheduling.SongVocalRange {
	sorted := make([]scheduling.SongVocalRange, 0, len(ranges))
	for _, r := range ranges {
		sorted = append(sorted, r)
	}
	sort.Slice(sorted, func(i, j int) bool {
		if !sorted[i].CreatedAt.Equal(sorted[j].CreatedAt) {
			return sorted[i].CreatedAt.After(sorted[j].CreatedAt)
		}
		return sorted[i].SongID < sorted[j].SongID
	})
	return sorted
}

func (sc *SchedulingController) SetSongVocalRange(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)

	var body struct {
		SongID       string `form:"song_id"`
		ReferenceKey string `form:"reference_key"`
		Lowest       string `form:"lowest_note"`
		Highest      string `form:"highest_note"`
	}
	errMsg := ""
	if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else if body.SongID == "" {
		errMsg = "song is required"
	} else if _, ok := scheduling.ParseKeyPitchClass(body.ReferenceKey); !ok {
		errMsg = fmt.Sprintf("%q isn't a key - try something like G, Bb, F#m", body.ReferenceKey)
	} else if lowPC, lowOct, ok := scheduling.ParseNote(body.Lowest); !ok {
		errMsg = fmt.Sprintf("%q isn't a note - try something like G3", body.Lowest)
	} else if highPC, highOct, ok := scheduling.ParseNote(body.Highest); !ok {
		errMsg = fmt.Sprintf("%q isn't a note - try something like D5", body.Highest)
	} else if scheduling.AbsoluteSemitone(lowPC, lowOct) > scheduling.AbsoluteSemitone(highPC, highOct) {
		errMsg = "lowest note must be at or below the highest note"
	} else {
		// ok already confirmed above via ParseKeyPitchClass on the same
		// string - ComputeVocalRangeOffsets re-derives it, it doesn't
		// re-validate.
		lowOffset, highOffset, _ := scheduling.ComputeVocalRangeOffsets(body.ReferenceKey, lowPC, lowOct, highPC, highOct)
		if _, err := sc.store.SetSongVocalRange(c.Context(), orgID, scheduling.SetSongVocalRangeParams{
			SongID:              body.SongID,
			ReferenceKey:        body.ReferenceKey,
			LowOffsetSemitones:  lowOffset,
			HighOffsetSemitones: highOffset,
		}); err != nil {
			errMsg = err.Error()
		}
	}

	rangeMap, err := sc.store.ListSongVocalRanges(c.Context(), orgID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}
	ranges := sortedSongVocalRanges(rangeMap)

	songs, _ := fetchActiveSongs(middleware.RequestContext(c), orgID)

	return render(c, views.SongVocalRangesTable(ranges, songTitleMap(songs), errMsg))
}

func (sc *SchedulingController) DeleteSongVocalRange(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)

	id, err := c.ParamsInt("id")
	errMsg := ""
	if err != nil {
		errMsg = "id must be an integer"
	} else if delErr := sc.store.DeleteSongVocalRange(c.Context(), orgID, int64(id)); delErr != nil && !errors.Is(delErr, sql.ErrNoRows) {
		errMsg = delErr.Error()
	}

	rangeMap, err := sc.store.ListSongVocalRanges(c.Context(), orgID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}
	ranges := sortedSongVocalRanges(rangeMap)

	songs, _ := fetchActiveSongs(middleware.RequestContext(c), orgID)

	return render(c, views.SongVocalRangesTable(ranges, songTitleMap(songs), errMsg))
}
