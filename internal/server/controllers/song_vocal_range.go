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
		return render(c, views.SongVocalRanges(personName, orgName, songs, nil, songTitleMap(songs), nil, err.Error()))
	}
	ranges := sortedSongVocalRanges(rangeMap)
	currentKeys := sc.currentKeysBySong(ctx, orgID, ranges)

	errMsg := ""
	if songsErr != nil {
		errMsg = "loading songs from Planning Center: " + songsErr.Error()
	}

	return render(c, views.SongVocalRanges(personName, orgName, songs, ranges, songTitleMap(songs), currentKeys, errMsg))
}

// currentKeysBySong resolves each listed range's song to its current
// cached PCO arrangement key (SongArrangementInfo.Key), for
// SongVocalRangesTable's "current key" transposed line - see that
// component's own doc comment. One bulk cache-only read
// (scheduling.SongArrangementInfo) regardless of how many ranges are
// listed, then a cache lookup per range via resolveArrangementCacheInfo
// (scheduling.go) - the existing enqueue-on-miss contract, never a live
// blocking PCO call (CLAUDE.md's PCO API calls section is explicit that a
// render path must not add one when this pattern already exists). Only
// looks up the songs actually passed in - this page only ever lists songs
// with a stored vocal range on file, a small subset of the whole active
// song library, not a full-library fan-out (see docs/pco-api-risks.md).
// A song whose cache entry is missing (enqueues a background sync) or has
// an empty Key (PCO has none set) is simply absent from the returned map -
// callers treat that the same "quiet skip" way SongVocalRangesTable's own
// doc comment describes.
func (sc *SchedulingController) currentKeysBySong(ctx context.Context, organizationID string, ranges []scheduling.SongVocalRange) map[string]string {
	arrangementCache, _ := sc.store.SongArrangementInfo(ctx, organizationID)

	keys := make(map[string]string, len(ranges))
	for _, r := range ranges {
		info := resolveArrangementCacheInfo(ctx, sc.store, organizationID, arrangementCache, r.SongID)
		if info.Key != "" {
			keys[r.SongID] = info.Key
		}
	}
	return keys
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

	// ok already confirmed above via ParseKeyPitchClass on the same
	// string - ComputeVocalRangeOffsets re-derives it, it doesn't
	// re-validate.
	lowOffset, highOffset, _ := scheduling.ComputeVocalRangeOffsets(referenceKey, lowPC, lowOct, highPC, highOct)
	if _, err := sc.store.SetSongVocalRange(ctx, organizationID, scheduling.SetSongVocalRangeParams{
		SongID:              songID,
		ReferenceKey:        referenceKey,
		LowOffsetSemitones:  lowOffset,
		HighOffsetSemitones: highOffset,
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
	currentKeys := sc.currentKeysBySong(ctx, orgID, ranges)

	songs, _ := fetchActiveSongs(ctx, orgID)

	return render(c, views.SongVocalRangesTable(ranges, songTitleMap(songs), currentKeys, errMsg))
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
	currentKeys := sc.currentKeysBySong(ctx, orgID, ranges)

	songs, _ := fetchActiveSongs(ctx, orgID)

	return render(c, views.SongVocalRangesTable(ranges, songTitleMap(songs), currentKeys, errMsg))
}
