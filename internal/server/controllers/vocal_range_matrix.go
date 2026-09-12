package controllers

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"pco-assistant/internal/scheduling"
	"pco-assistant/internal/server/middleware"
	views "pco-assistant/internal/views/scheduling"

	"github.com/gofiber/fiber/v2"
)

// VocalRangeMatrixPage renders the vocal-range/key matrix view: one plan's
// roster (rows) crossed against that plan's own set songs (columns), each
// cell showing at a glance whether that person's stored range fits the
// song's own effective (transposed) range - the same fits/tip/mismatch
// verdict the Songs tab's own per-song mismatch badge
// (vocalRangeWarningForSong, sets_builder.go) already computes per
// vocalist, laid out as a grid instead of one badge per row. See
// docs/plans/not-started/vocal-range-matrix-and-crowdsourced-default-plan.md's
// "The matrix view" section (now shipped - this file).
//
// Deliberately scoped to one plan at a time, matching how the Build page
// and roster builder already scope everything - a whole-library or
// whole-org matrix was explicitly out of scope for this pass. Linked from
// the Build page (build.templ's nav-links row, alongside "Run sheet")
// rather than a new top-level nav item or a third Songs/Team tab - see
// vocal_range_matrix.templ's own doc comment for why a tab was rejected.
func (sc *SchedulingController) VocalRangeMatrixPage(c *fiber.Ctx) error {
	_, personName := middleware.CurrentPerson(c)
	orgName := middleware.CurrentOrganization(c)
	orgID := middleware.CurrentOrganizationID(c)

	ctx := middleware.RequestContext(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")

	data := sc.buildVocalRangeMatrixData(ctx, orgID, serviceTypeID, planID)
	return render(c, views.VocalRangeMatrix(personName, orgName, data))
}

// buildVocalRangeMatrixData assembles views.VocalRangeMatrixData - see
// that type's own doc comment for its shape. Reuses buildSetBuilderData
// for the plan's own set songs (each already carrying its effective,
// transposed vocal range - see SetSongView.VocalRangeHasPlayableNotes/
// VocalRangeLowPitchClass etc.) rather than recomputing that
// transposition a second time; the roster/person-range reads below are
// the same local-DB-only lookups buildSetBuilderData's own vocalist-check
// section already makes (see that function's doc comment on
// vocalistPersonIDs/personVocalRanges) - duplicated here since that
// section doesn't expose its person id list on SetBuilderData, and this
// is the only other page that needs it. personNameMapForIDs is the one
// PCO call, batched and bounded to this plan's own roster size, same
// established pattern rosterPersonNames/vocalRangeCheckNames already use.
func (sc *SchedulingController) buildVocalRangeMatrixData(ctx context.Context, organizationID, serviceTypeID, planID string) views.VocalRangeMatrixData {
	setData := sc.buildSetBuilderData(ctx, organizationID, serviceTypeID, planID)

	data := views.VocalRangeMatrixData{
		ServiceTypeID: serviceTypeID,
		PlanID:        planID,
		PlanTitle:     setData.PlanTitle,
		PlanDates:     setData.PlanDates,
		ErrMsg:        setData.ErrMsg,
	}

	for _, row := range setData.ServiceOrder {
		if !row.IsSong {
			continue
		}
		data.Songs = append(data.Songs, views.VocalRangeMatrixSong{
			SongID:         row.Song.SongID,
			Title:          row.Song.Title,
			Key:            row.Song.Key,
			RangeDisplay:   row.Song.VocalRangeDisplay,
			HasRange:       row.Song.VocalRangeHasPlayableNotes,
			LowPitchClass:  row.Song.VocalRangeLowPitchClass,
			LowOctave:      row.Song.VocalRangeLowOctave,
			HighPitchClass: row.Song.VocalRangeHighPitchClass,
			HighOctave:     row.Song.VocalRangeHighOctave,
		})
	}

	// Roster: every unique person assigned to any position on this plan's
	// roster - the matrix's rows (see VocalRangeMatrixData.People's own
	// doc comment on why this is literally "the roster," not a
	// vocalist-only filter). Local-DB only, no PCO call - best-effort like
	// buildSetBuilderData's own equivalent lookup: a failure here just
	// means an empty matrix, not a broken page.
	var personIDs []string
	seen := make(map[string]bool)
	if roster, found, err := sc.store.FindRoster(ctx, organizationID, serviceTypeID, planID); err == nil && found {
		if assignments, err := sc.store.ListRosterAssignments(ctx, roster.ID); err == nil {
			for _, a := range assignments {
				if a.PersonID == "" || seen[a.PersonID] {
					continue
				}
				seen[a.PersonID] = true
				personIDs = append(personIDs, a.PersonID)
			}
		}
	}

	personRanges, _ := sc.store.ListPersonVocalRanges(ctx, organizationID)
	personNames := personNameMapForIDs(ctx, personIDs)

	for _, id := range personIDs {
		person := views.VocalRangeMatrixPerson{
			PersonID: id,
			Name:     titleOrID(personNames, id),
		}
		if pr, ok := personRanges[id]; ok {
			person.HasRange = true
			person.RangeDisplay = fmt.Sprintf("%s-%s", formatVocalNote(pr.LowestPitchClass, pr.LowestOctave), formatVocalNote(pr.HighestPitchClass, pr.HighestOctave))
		}
		data.People = append(data.People, person)
	}
	sort.Slice(data.People, func(i, j int) bool {
		return strings.ToLower(data.People[i].Name) < strings.ToLower(data.People[j].Name)
	})

	data.Cells = make(map[string]map[string]views.VocalRangeMatrixCell, len(data.People))
	for _, person := range data.People {
		row := make(map[string]views.VocalRangeMatrixCell, len(data.Songs))
		for _, song := range data.Songs {
			row[song.SongID] = vocalRangeMatrixCellFor(song, person, personRanges)
		}
		data.Cells[person.PersonID] = row
	}

	return data
}

// vocalRangeMatrixCellFor is one grid cell's verdict - Unknown whenever
// either side has no usable stored range (see views.VocalRangeMatrixCell's
// own doc comment on why that's a neutral state, not a false pass or
// warning), otherwise classifyVocalRangeCell's own fits/tip/mismatch rule
// (sets_builder.go) - the exact same classification
// vocalRangeWarningForSong applies per vocalist for the Songs tab's own
// badge, reused verbatim here rather than a second comparison, so this
// matrix can never disagree with that badge about the same person/song
// pair.
func vocalRangeMatrixCellFor(song views.VocalRangeMatrixSong, person views.VocalRangeMatrixPerson, personRanges map[string]scheduling.PersonVocalRange) views.VocalRangeMatrixCell {
	if !song.HasRange || !person.HasRange {
		return views.VocalRangeMatrixCell{Verdict: views.VocalRangeMatrixUnknown}
	}

	pr := personRanges[person.PersonID]
	personLow := scheduling.AbsoluteSemitone(pr.LowestPitchClass, pr.LowestOctave)
	personHigh := scheduling.AbsoluteSemitone(pr.HighestPitchClass, pr.HighestOctave)
	songLow := scheduling.AbsoluteSemitone(song.LowPitchClass, song.LowOctave)
	songHigh := scheduling.AbsoluteSemitone(song.HighPitchClass, song.HighOctave)

	result := classifyVocalRangeCell(songLow, songHigh, personLow, personHigh)
	switch result.Verdict {
	case vocalRangeCellFits:
		return views.VocalRangeMatrixCell{Verdict: views.VocalRangeMatrixFits}
	case vocalRangeCellTip:
		return views.VocalRangeMatrixCell{
			Verdict: views.VocalRangeMatrixTip,
			Message: fmt.Sprintf("Fits if sung an octave %s than written.", result.Direction),
		}
	default:
		return views.VocalRangeMatrixCell{
			Verdict: views.VocalRangeMatrixMismatch,
			Message: fmt.Sprintf("Needs to reach %s.", result.Reach),
		}
	}
}
