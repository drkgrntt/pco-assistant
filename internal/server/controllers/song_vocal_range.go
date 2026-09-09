package controllers

import (
	"context"
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
	ctx := middleware.RequestContext(c)
	defer triggerSongInfoQueueSync(ctx, sc.store, orgID)

	songs, songsErr := fetchActiveSongs(ctx, orgID)

	rangeMap, err := sc.store.ListSongVocalRanges(ctx, orgID)
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

// setSongVocalRangeFromForm parses/validates a reference key + low/high
// note string triple - the same three fields both this setup page's own
// form and the Build page's quick-add-panel/kebab-menu counterpart submit
// (see QuickAddSongVocalRange, sets_detail.go) - and upserts
// scheduling.Store's SetSongVocalRange. Factored out of SetSongVocalRange
// below so the two call sites' validation/anchoring logic can never drift
// out of sync with each other, the same "one shared setter, two posting
// surfaces" shape QuickAddSongTag/QuickAddSongRelationship/QuickAddSongRole
// already established for their own Store methods. Returns "" on success.
func (sc *SchedulingController) setSongVocalRangeFromForm(ctx context.Context, organizationID, songID, referenceKey, lowest, highest string) string {
	if songID == "" {
		return "song is required"
	}
	if _, ok := scheduling.ParseKeyPitchClass(referenceKey); !ok {
		return fmt.Sprintf("%q isn't a key - try something like G, Bb, F#m", referenceKey)
	}
	lowPC, lowOct, ok := scheduling.ParseNote(lowest)
	if !ok {
		return fmt.Sprintf("%q isn't a note - try something like G3", lowest)
	}
	highPC, highOct, ok := scheduling.ParseNote(highest)
	if !ok {
		return fmt.Sprintf("%q isn't a note - try something like D5", highest)
	}
	if scheduling.AbsoluteSemitone(lowPC, lowOct) > scheduling.AbsoluteSemitone(highPC, highOct) {
		return "lowest note must be at or below the highest note"
	}

	if _, err := sc.store.SetSongVocalRange(ctx, organizationID, scheduling.SetSongVocalRangeParams{
		SongID:            songID,
		ReferenceKey:      referenceKey,
		LowestPitchClass:  lowPC,
		LowestOctave:      lowOct,
		HighestPitchClass: highPC,
		HighestOctave:     highOct,
	}); err != nil {
		return err.Error()
	}
	return ""
}

func (sc *SchedulingController) SetSongVocalRange(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	ctx := middleware.RequestContext(c)
	defer triggerSongInfoQueueSync(ctx, sc.store, orgID)

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

	rangeMap, err := sc.store.ListSongVocalRanges(ctx, orgID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}
	ranges := sortedSongVocalRanges(rangeMap)

	songs, _ := fetchActiveSongs(ctx, orgID)

	return render(c, views.SongVocalRangesTable(ranges, songTitleMap(songs), errMsg))
}

func (sc *SchedulingController) DeleteSongVocalRange(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	ctx := middleware.RequestContext(c)
	defer triggerSongInfoQueueSync(ctx, sc.store, orgID)

	id, err := c.ParamsInt("id")
	errMsg := ""
	if err != nil {
		errMsg = "id must be an integer"
	} else if delErr := sc.store.DeleteSongVocalRange(ctx, orgID, int64(id)); delErr != nil && !errors.Is(delErr, sql.ErrNoRows) {
		errMsg = delErr.Error()
	}

	rangeMap, err := sc.store.ListSongVocalRanges(ctx, orgID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}
	ranges := sortedSongVocalRanges(rangeMap)

	songs, _ := fetchActiveSongs(ctx, orgID)

	return render(c, views.SongVocalRangesTable(ranges, songTitleMap(songs), errMsg))
}
