package controllers

// Assembling views.SetBuilderData: merging local set songs into the plan's
// real order of service, plus the pure helpers this leans on (also covered
// by sets_builder_test.go).

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"pco-assistant/internal/logger"
	"pco-assistant/internal/scheduling"
	"pco-assistant/internal/server/middleware"
	views "pco-assistant/internal/views/scheduling"

	pco "github.com/drkgrntt/go-pco"
	"github.com/gofiber/fiber/v2"
)

func (sc *SchedulingController) buildSetBuilderDataWithErr(ctx context.Context, organizationID, serviceTypeID, planID, errMsg string) views.SetBuilderData {
	data := sc.buildSetBuilderData(ctx, organizationID, serviceTypeID, planID)
	if errMsg != "" {
		data.ErrMsg = appendErr(errMsg, data.ErrMsg)
	}
	return data
}

// buildSetBuilderData gathers everything the set builder page (and every
// htmx fragment it swaps after an add/remove/move) needs to render: the
// plan header, the draft set's songs, flows_well/same_theme suggestions
// based on the last song added, avoid_pairing conflicts, and the full
// not-yet-added song pool annotated with opener/response tags and any
// conflicts, for the search/browse list.
//
// Also triggers the Song Info background queue-drain sync
// (triggerSongInfoQueueSync, song_arrangement_cache.go) right before
// returning, covering every render this function backs - including
// buildSetBuilderDataWithErr's, which just wraps this one - rather than
// requiring each of this function's ~20-plus call sites to remember to
// call the trigger themselves the way person_data_cache.go's
// triggerPersonDataSync requires of its own, much smaller set of callers.
// See triggerSongInfoQueueSync's own doc comment for the full reasoning
// (docs/plans/shipped/pco-call-efficiency-rollout-plan.md's Phase 2 "trigger mechanism"
// decision). Deferred so it still runs on this function's several early
// "loading X failed" returns below, not just the final one.
func (sc *SchedulingController) buildSetBuilderData(ctx context.Context, organizationID, serviceTypeID, planID string) views.SetBuilderData {
	defer triggerSongInfoQueueSync(ctx, sc.store, organizationID)

	data := views.SetBuilderData{ServiceTypeID: serviceTypeID, PlanID: planID}

	if plan, err := pco.GetPlan(ctx, serviceTypeID, planID); err == nil {
		data.PlanTitle = plan.Data.Attributes.Title
		data.PlanDates = plan.Data.Attributes.Dates
		// PreviousPlan/NextPlan are PCO's own relationship on the plan
		// itself (chronologically adjacent within this service type) -
		// reused as-is rather than fetched separately, so the
		// Previous/Next buttons (build.templ) cost nothing beyond this
		// GetPlan call the page already makes.
		if rel := plan.Data.Relationships.PreviousPlan.Data; rel != nil {
			data.PrevPlanID = rel.ID
		}
		if rel := plan.Data.Relationships.NextPlan.Data; rel != nil {
			data.NextPlanID = rel.ID
		}
		// ReadOnly mirrors buildRosterBuilderData's own planDate-derived
		// check (rosters_builder.go) - a plan whose sort_date has already
		// passed is shown for browsing only, no Publish/Discard/PCO-sync
		// actions (see SetBuilderData.ReadOnly). Server-derived from the
		// plan's own date rather than a client-supplied flag, so it can't
		// be spoofed and stays correct even for a plan that was still
		// upcoming when this page was first opened.
		data.ReadOnly = !plan.Data.Attributes.SortDate.IsZero() && plan.Data.Attributes.SortDate.Before(time.Now())
	} else {
		data.ErrMsg = "loading plan from Planning Center: " + err.Error()
	}

	set, err := sc.store.GetOrCreateSet(ctx, organizationID, serviceTypeID, planID)
	if err != nil {
		data.ErrMsg = appendErr(data.ErrMsg, err.Error())
		return data
	}

	setSongs, err := sc.store.ListSetSongs(ctx, set.ID)
	if err != nil {
		data.ErrMsg = appendErr(data.ErrMsg, err.Error())
		return data
	}

	songs, songsErr := fetchActiveSongs(ctx, organizationID)
	if songsErr != nil {
		data.ErrMsg = appendErr(data.ErrMsg, "loading songs from Planning Center: "+songsErr.Error())
	}
	titles := songTitleMap(songs)

	// SetSongs/pendingDeletions (below) reference songs already scheduled
	// locally, as opposed to AllSongs (songs, above - the add-a-song
	// candidate pool), which is deliberately active-only with no caveats.
	// A song scheduled while active, then archived in PCO later, would
	// otherwise silently fall back to titleOrID's raw-ID display for
	// every one of these - so backfill titles for exactly the local
	// set-song rows that reference it, with one targeted GetSong call per
	// still-missing id (typically zero in practice).
	setSongIDs := make([]string, 0, len(setSongs))
	for _, ss := range setSongs {
		setSongIDs = append(setSongIDs, ss.SongID)
	}
	resolveMissingTitles(ctx, titles, setSongIDs)

	// New-to-the-church detection: the first time this app ever sees a song
	// with no Planning Center scheduling history at all, permanently mark it
	// for reinforcement tracking - see tracked_new_songs/TrackNewSong. Done
	// for every fetched song, not just Candidates, since a song can be
	// genuinely new to the church whether or not it's already in this
	// draft. Best-effort: a missed insert just means one song doesn't get
	// reinforcement nudges yet, not a broken page - it'll be caught on a
	// later render.
	for _, song := range songs {
		if song.Attributes.LastScheduledAt == nil {
			_ = sc.store.TrackNewSong(ctx, organizationID, song.ID)
		}
	}

	roleTypes, _ := sc.store.ListSongRoleTypes(ctx)
	roleTypeLabels := songRoleTypeLabelMap(roleTypes)
	allRoles, _ := sc.store.ListSongRoles(ctx, organizationID, "", "")
	rolesBySong := songRolesBySong(allRoles)

	// Best-effort, same as titles/roles above: tags are a search/display
	// nicety on the candidate list, so a lookup failure just means no tag
	// badges/search text rather than failing the whole page.
	allTags, _ := sc.store.ListSongTags(ctx, organizationID, "", "")
	tagCategories, _ := sc.store.ListSongTagCategories(ctx)
	tagsBySong := songTagsBySong(allTags)
	tagCategoryLabels := songTagCategoryLabelMap(tagCategories)

	// SongsToReinforce/NewSongLabel below key off these two: which songs are
	// tracked as genuinely new to the church (see the write-side loop
	// above), and how many distinct plans each song's actually been
	// published into so far - see PublishedSongAppearanceCounts. Best-effort
	// like the lookups above.
	trackedNew, _ := sc.store.TrackedNewSongs(ctx, organizationID)
	appearanceCounts, _ := sc.store.PublishedSongAppearanceCounts(ctx, organizationID)

	// arrangementCache backs both the order-of-service's ArrangementInfo
	// (resolveArrangementInfo falls back to a live PCO fetch + backfill on
	// a miss - see its own doc comment) and the candidate list's, which is
	// cache-only (no live fetch - see the Candidates loop below). One bulk
	// read regardless of how many songs actually need it.
	arrangementCache, _ := sc.store.SongArrangementInfo(ctx, organizationID)

	// attachmentCache backs the detail card's "Files" list (SetSongView.
	// Attachments) - cache-only, same shape as arrangementCache above, one
	// bulk read for the whole page render. See
	// docs/plans/shipped/song-lead-sheet-attachments-plan.md.
	attachmentCache, _ := sc.store.SongAttachments(ctx, organizationID)

	// vocalRangesBySong backs the detail card's "Vocal range" line
	// (SetSongView.VocalRangeDisplay) - one bulk local-DB read, same
	// shape as arrangementCache/attachmentCache above, no PCO call. See
	// docs/plans/in-progress/vocal-range-plan.md's Phase 2 section.
	vocalRangesBySong, _ := sc.store.ListSongVocalRanges(ctx, organizationID)

	// vocalistPersonIDs/personVocalRanges/vocalRangeCheckNames back Phase 3's
	// per-row mismatch warning (SetSongView.VocalRangeWarningLabel - see
	// vocalRangeWarningForSong below). "Who counts as a vocalist to check"
	// is exactly this plan's roster assignments (not an eligibility pool,
	// and not a team/position-name heuristic) intersected with who actually
	// has a PersonVocalRange on file - see docs/plans/in-progress/
	// vocal-range-plan.md's Phase 3 "who counts" section. Both roster and
	// person-range reads are local-DB only, same as vocalRangesBySong
	// above; vocalRangeCheckNames is the one new PCO call this phase adds
	// (personNameMapForIDs, same batched where[id]=... pattern leaderNames
	// below already uses), and only for the subset of the roster that both
	// has a range on file - bounded to a plan's own roster size, never the
	// whole org, and 0 calls whenever nobody assigned has a range on file.
	// Best-effort throughout, same tolerance as every other lookup here: a
	// roster/range lookup failure just means no mismatch badges render,
	// not a broken page.
	var vocalistPersonIDs []string
	if roster, found, err := sc.store.FindRoster(ctx, organizationID, serviceTypeID, planID); err == nil && found {
		if assignments, err := sc.store.ListRosterAssignments(ctx, roster.ID); err == nil {
			for _, a := range assignments {
				vocalistPersonIDs = append(vocalistPersonIDs, a.PersonID)
			}
		}
	}
	personVocalRanges, _ := sc.store.ListPersonVocalRanges(ctx, organizationID)
	vocalRangeCheckNames := personNameMapForIDs(ctx, vocalistsWithRangesOnFile(vocalistPersonIDs, personVocalRanges))

	// data.RosterPersonOptions is who's eligible to be picked as a song
	// leader - see its own doc comment (set_types.go) for why this is
	// deliberately the exact same roster-assignments list as
	// vocalistPersonIDs above, not a fresh query. Resolving names costs one
	// more batched where[id]=... call (personNameMapForIDs), bounded by
	// this plan's own roster size - 0 calls when nobody's on the roster
	// yet, same "opt-in cost" shape as vocalRangeCheckNames above.
	rosterPersonNames := personNameMapForIDs(ctx, vocalistPersonIDs)
	data.RosterPersonOptions = rosterPersonOptions(vocalistPersonIDs, rosterPersonNames)

	// AllSongs/TagCategories/RelationshipTypes/RoleTypes back the "Quick
	// add" panel's <select> pickers (see views.SetBuilderData) - best-effort
	// like the lookups above, since a failure here only costs that one
	// panel's dropdowns, not the page.
	data.AllSongs = songs
	data.TagCategories = tagCategories
	data.RelationshipTypes, _ = sc.store.ListSongRelationshipTypes(ctx)
	data.RoleTypes = roleTypes
	data.TagValuesByCategory, _ = sc.store.DistinctSongTagValues(ctx, organizationID)
	data.AllSongVocalRanges = vocalRangesBySong

	inSet := make(map[string]bool, len(setSongs))
	for _, ss := range setSongs {
		inSet[ss.SongID] = true
		if ss.PCOItemID == "" {
			data.UnpublishedCount++
		}
		if ss.Moved {
			data.ReorderPending = true
		}
		if ss.InfoPending {
			data.InfoPendingCount++
		}
		roleViews, _ := songRoleViews(rolesBySong, roleTypeLabels, ss.SongID)
		data.SetSongs = append(data.SetSongs, views.SetSongView{
			ID:           ss.ID,
			SongID:       ss.SongID,
			Title:        titleOrID(titles, ss.SongID),
			Position:     ss.Position,
			Roles:        roleViews,
			Published:    ss.PCOItemID != "",
			AnchorItemID: ss.AnchorItemID,
			Moved:        ss.Moved,
			InfoPending:  ss.InfoPending,
		})
	}

	// BrowsableSongs - see its own doc comment on SetBuilderData. Built once
	// here from songs/inSet, already in hand, rather than a fresh fetch;
	// searchCandidates builds the identical list on its own early-return
	// path for this same list's htmx re-renders.
	data.BrowsableSongs = buildBrowsableSongs(songs, inSet)

	pendingDeletions, err := sc.store.ListPendingDeletions(ctx, set.ID)
	if err != nil {
		data.ErrMsg = appendErr(data.ErrMsg, err.Error())
	}
	// Same backfill reasoning as setSongIDs above - a tombstoned set-song
	// row can just as easily reference a since-archived song.
	pendingDeletionIDs := make([]string, 0, len(pendingDeletions))
	for _, pd := range pendingDeletions {
		pendingDeletionIDs = append(pendingDeletionIDs, pd.SongID)
	}
	resolveMissingTitles(ctx, titles, pendingDeletionIDs)
	data.PendingDeleteCount = len(pendingDeletions)

	// PendingChanges spells out exactly what's different from Planning
	// Center right now, one line per change - UnpublishedCount/
	// PendingDeleteCount/ReorderPending (used for the Publish/Discard
	// buttons themselves) only ever say *how many* things changed, not
	// *what*, which is easy to lose track of days after actually making
	// the edit. Moved songs are named individually (not just "the order
	// changed") since SetSong.Moved is now tracked per row.
	for _, ss := range data.SetSongs {
		if !ss.Published {
			data.PendingChanges = append(data.PendingChanges, fmt.Sprintf("Added: %s", ss.Title))
		} else if ss.Moved {
			data.PendingChanges = append(data.PendingChanges, fmt.Sprintf("Moved: %s", ss.Title))
		} else if ss.InfoPending {
			data.PendingChanges = append(data.PendingChanges, fmt.Sprintf("Note/key updated: %s", ss.Title))
		}
	}
	for _, ss := range pendingDeletions {
		data.PendingChanges = append(data.PendingChanges, fmt.Sprintf("Removed: %s", titleOrID(titles, ss.SongID)))
	}

	// history covers both the repeat-warning check further down and the
	// service-length warning below - one GetPlans/GetItems round trip
	// against this service type's two most recent past plans serves both,
	// rather than fetching the same history twice. Moved up here (rather
	// than fetched right where the repeat check gets built) so
	// history.AverageLength/RepeatedSongs are already in hand by the time
	// the ServiceOrder block below needs them.
	history := fetchRecentPlanHistory(ctx, serviceTypeID)

	// candidateConflicts/existing are fetched up front too - moved from
	// where this used to be fetched (right before the Suggestions loop
	// further down, where candidateConflicts is still needed unchanged) so
	// existing's same-set avoid-pairing conflicts can be attributed onto
	// each song's own row (see conflictTitlesBySong and
	// SetSongView.ConflictTitles, and docs/plans/shipped/
	// in-place-set-warnings-plan.md) instead of a separate top-of-page
	// list, before ServiceOrder is built below.
	candidateConflicts, existing, err := sc.store.AvoidPairings(ctx, organizationID, set.ID)
	if err != nil {
		data.ErrMsg = appendErr(data.ErrMsg, err.Error())
	}
	// conflictTitlesBySong attributes each same-set avoid-pairing conflict
	// back to *both* songs in the pair, by title - existing reports each
	// pair once (SongID < ConflictsWith, see computeAvoidPairings), but a
	// per-row badge needs to render on either side naming the other, the
	// same shape CandidateSongView.ConflictTitles already gives a
	// not-yet-added candidate (see candidateSongView).
	conflictTitlesBySong := map[string][]string{}
	for _, ex := range existing {
		conflictTitlesBySong[ex.SongID] = append(conflictTitlesBySong[ex.SongID], titleOrID(titles, ex.ConflictsWith))
		conflictTitlesBySong[ex.ConflictsWith] = append(conflictTitlesBySong[ex.ConflictsWith], titleOrID(titles, ex.SongID))
	}

	// ServiceOrder: the plan's full order, with this set's own songs
	// interleaved at their anchored position (see mergeServiceOrder) -
	// replaces the old song-only "current set" list, since seeing (and
	// placing songs against) the whole order of service, template headers
	// included, is the point.
	if pcoItems, err := pco.GetItems(ctx, serviceTypeID, planID, &pco.ItemsParams{Include: []string{"item_notes"}}); err != nil {
		data.ErrMsg = appendErr(data.ErrMsg, "loading plan items from Planning Center: "+err.Error())
	} else {
		sort.Slice(pcoItems.Data, func(i, j int) bool {
			return pcoItems.Data[i].Attributes.Sequence < pcoItems.Data[j].Attributes.Sequence
		})

		// PCOSyncFingerprint - see its own doc comment on SetBuilderData.
		// Computed from this exact pcoItems response, already in hand for
		// ServiceOrder below - no extra PCO call, same reasoning as
		// MergeAvailableCount further down.
		data.PCOSyncFingerprint = pcoItemsFingerprint(pcoItems.Data)

		merged := mergeServiceOrder(pcoItems.Data, setSongs)

		// transitionsByRow: keyTransitionWarnings flags two songs that are
		// truly back-to-back in merged with no transition between their
		// keys - see that function's own doc comment. Computed here, ahead
		// of renderServiceOrder just below, so the transition can be
		// attributed onto the *second* song's own row (SetSongView.
		// KeyTransitionLabel/KeyTransitionMessage) instead of a separate
		// top-of-page list - see docs/plans/shipped/
		// in-place-set-warnings-plan.md's resolved rendering decision.
		transitionsByRow := make(map[int64]keyTransitionWarning, 1)
		for _, w := range keyTransitionWarnings(merged, arrangementCache, titles) {
			transitionsByRow[w.SongRowID] = w
		}

		// transitionNotesBySongID/transitionNotePromptsByRow: the "post-
		// service debrief log / transition notes" feature (see
		// docs/plans/shipped/transition-notes-plan.md) - one
		// ListTransitionNotesForSong(ctx, orgID, "") call fetches this org's
		// entire table (small by design - see that plan's Schema section),
		// then everything else here is pure Go reduction over that one
		// result: transitionNotesBySong feeds each song's own detail-card
		// log (SetSongView.TransitionNotes), and
		// latestTransitionNotesByKey+transitionNotePrompts feed the
		// resurfacing banner (SetSongView.TransitionNotePrompts), keyed by
		// row id the same way transitionsByRow is above. No PCO call at
		// all, and no extra local query beyond this one. Best-effort like
		// the lookups above.
		allTransitionNotes, _ := sc.store.ListTransitionNotesForSong(ctx, organizationID, "")
		transitionNotesBySongID := transitionNotesBySong(allTransitionNotes)
		latestTransitionNotes := latestTransitionNotesByKey(allTransitionNotes)
		transitionNotePromptsByRow := make(map[int64][]transitionNotePrompt, 1)
		for _, p := range transitionNotePrompts(merged, latestTransitionNotes, titles) {
			transitionNotePromptsByRow[p.SongRowID] = append(transitionNotePromptsByRow[p.SongRowID], p)
		}

		// Fetched once per render, not per song - see renderServiceOrder's
		// own doc comment on noteCategories.
		noteCategories, _ := pco.GetItemNoteCategories(ctx, serviceTypeID, nil)
		// Sideloaded on the GetItems call above (Include: []string{"item_notes"})
		// rather than one GetItemNotes call per published song - see
		// itemNotesFromIncluded's own doc comment.
		itemNotes := itemNotesFromIncluded(pcoItems.Included)
		// leaderNames resolves every distinct song leader's PCO person id
		// to a display name in one batched where[id]=... call
		// (personNameMapForIDs) - empty (and free) when no song in this
		// set has a leader set. Feeds SetSongView.LeaderPersonName below,
		// the same "resolve once here, not per-row" shape as titles.
		leaderNames := personNameMapForIDs(ctx, distinctLeaderPersonIDs(setSongs))
		data.ServiceOrder = renderServiceOrder(ctx, sc.store, organizationID, arrangementCache, attachmentCache, vocalRangesBySong, noteCategories.Data, itemNotes, merged, titles, rolesBySong, roleTypeLabels, history.RepeatedSongs, conflictTitlesBySong, transitionsByRow, leaderNames, transitionNotesBySongID, transitionNotePromptsByRow, vocalistPersonIDs, personVocalRanges, vocalRangeCheckNames)

		// ServiceLengthWarning: the one warning that stays a standalone,
		// whole-plan banner rather than moving onto a row - it isn't
		// attributable to any one song or pair (see SetBuilderData's own
		// doc comment). Works off merged/arrangementCache, already in hand
		// here, and history.AverageLength, already fetched above - no new
		// PCO calls.
		data.ServiceLengthWarning = serviceLengthWarning(serviceOrderLengthSeconds(merged, arrangementCache), history.AverageLength)

		// Keep this service type's "where does the first song usually go"
		// memory (see PreferredFirstSongAnchorTitle, and firstSongFallbackAnchor
		// which reads it back for a fresh empty set) in sync with whatever's
		// actually true right now - every add or move of the first song
		// updates it immediately here, not just on a confirmed publish, since
		// this runs after every mutation (and on plain page views, which is
		// harmless - the upsert is idempotent). Deliberately walks merged
		// (the real interleaved order ServiceOrder is built from) for the
		// first row that's a song, rather than assuming setSongs[0] (lowest
		// SetSong.Position): Position is only a total order among set songs
		// themselves, established by whatever order they were spliced in -
		// it says nothing about where their anchor items actually fall
		// relative to each other, so a song anchored to a *later* header can
		// still end up with the lowest Position. Best-effort: a local memory
		// hint, not data sent to Planning Center, so a failure here doesn't
		// belong in data.ErrMsg.
		if len(setSongs) > 0 {
			anchorTitle := ""
			for _, row := range merged {
				if row.Song == nil {
					continue
				}
				if anchor := row.Song.AnchorItemID; anchor != "" {
					for _, item := range pcoItems.Data {
						if item.ID == anchor {
							anchorTitle = item.Attributes.Title
							if anchorTitle == "" {
								anchorTitle = item.Attributes.ItemType
							}
							break
						}
					}
				}
				break
			}
			_ = sc.store.RememberFirstSongAnchor(ctx, organizationID, serviceTypeID, anchorTitle)
		}

		// ImportableSongCount only matters the first time this set is opened:
		// once it has anything of its own (active or tombstoned - see
		// data.PendingDeleteCount), every song-linked item is either already
		// one of setSongs or was deliberately removed, so there's nothing
		// left to offer importing. Reuses pcoItems rather than a fresh PCO
		// call, since ServiceOrder just fetched the exact same list.
		if len(setSongs) == 0 && data.PendingDeleteCount == 0 {
			for _, item := range pcoItems.Data {
				if item.Attributes.ItemType == pco.ItemTypeSong {
					data.ImportableSongCount++
				}
			}
		}

		// MergeAvailableCount: same untracked-item walk MergeSet itself
		// does (deriveMergeImportRows), reusing pcoItems/setSongs/
		// pendingDeletions already in hand here - see its own doc comment
		// on SetBuilderData for why this doesn't collapse into
		// ImportableSongCount above.
		data.MergeAvailableCount = len(deriveMergeImportRows(pcoItems.Data, trackedPCOItemIDs(setSongs, pendingDeletions)))
	}

	// repeated: songs sung in both of the last two past plans for this
	// service type, straight from Planning Center's own history rather than
	// this tool's local sets - see fetchRecentPlanHistory. Surfaced both
	// against songs already in this set (SetSongView.RecentlyRepeated, a
	// per-row badge - see renderServiceOrder) and against not-yet-added
	// candidates (a badge here too, so it's visible before adding rather
	// than only after).
	repeated := history.RepeatedSongs

	suggestions, err := sc.store.Recommendations(ctx, organizationID, set.ID)
	if err != nil {
		data.ErrMsg = appendErr(data.ErrMsg, err.Error())
	}
	for _, sug := range suggestions {
		roleViews, rolesAttr := songRoleViews(rolesBySong, roleTypeLabels, sug.SongID)
		tagViews, tagsAttr := songTagViews(tagsBySong, tagCategoryLabels, sug.SongID)

		var conflictTitles []string
		for _, conflictSongID := range candidateConflicts[sug.SongID] {
			conflictTitles = append(conflictTitles, titleOrID(titles, conflictSongID))
		}

		var arrangementInfo string
		if info, ok := arrangementCache[sug.SongID]; ok {
			arrangementInfo = formatArrangementInfo(info)
		}

		data.Suggestions = append(data.Suggestions, views.SuggestionView{
			CandidateSongView: views.CandidateSongView{
				SongID:           sug.SongID,
				Title:            titleOrID(titles, sug.SongID),
				ConflictTitles:   conflictTitles,
				RecentlyRepeated: repeated[sug.SongID],
				NewSongLabel:     reinforcementLabel(trackedNew, appearanceCounts, sug.SongID),
				ArrangementInfo:  arrangementInfo,
				Roles:            roleViews,
				RolesAttr:        rolesAttr,
				Tags:             tagViews,
				TagsAttr:         tagsAttr,
			},
			RelationshipLabel: strings.Join(sug.RelationshipTypes, ", "),
		})
	}

	// lookups bundles everything candidateSongView needs to compute one
	// not-yet-added song's full rich view - built once here and reused by
	// BuildCandidatesSearch's own fetch of the same underlying data (see
	// that handler, further down this file) for the "Browse all songs"
	// search list.
	lookups := candidateLookups{
		titles:             titles,
		rolesBySong:        rolesBySong,
		roleTypeLabels:     roleTypeLabels,
		tagsBySong:         tagsBySong,
		tagCategoryLabels:  tagCategoryLabels,
		candidateConflicts: candidateConflicts,
		repeated:           repeated,
		trackedNew:         trackedNew,
		appearanceCounts:   appearanceCounts,
		arrangementCache:   arrangementCache,
	}

	for _, song := range songs {
		if inSet[song.ID] {
			continue
		}

		// ReplaceCandidates is cheap - just id+title - so it's still built
		// for the whole library on every render, unlike the rich
		// CandidateSongView computation below (see
		// views.SetBuilderData.ReplaceCandidates).
		data.ReplaceCandidates = append(data.ReplaceCandidates, views.SongOptionView{SongID: song.ID, Title: song.Attributes.Title})

		// The rest of this loop - roles/tags/conflicts/arrangement info -
		// used to run for every song in the library on every single Build
		// page render, just to populate the "Browse all songs" list (see
		// BuildCandidatesSearch's doc comment for why that's now a search
		// endpoint instead, computed only for actual matches). The one
		// thing left here that still needs it is SongsToReinforce, and only
		// for a song that's actually reinforcement-eligible - a small
		// fraction of a real library - so check that cheap map lookup first
		// and skip the rich computation entirely for everything else.
		newSongLabel := reinforcementLabel(trackedNew, appearanceCounts, song.ID)
		if newSongLabel == "" {
			continue
		}

		candidate := lookups.candidateSongView(song)

		// SongsToReinforce is its own list, not merged into data.Suggestions:
		// Suggestions is anchored to the last song in the set and would stay
		// empty on an empty set, but a still-new song deserves a nudge
		// regardless of what's already in the set - see set_types.go.
		switch newSongLabel {
		case "Sung once":
			data.SongsToReinforce = append(data.SongsToReinforce, views.SuggestionView{
				CandidateSongView: candidate,
				RelationshipLabel: "Sung once - one more to make it stick",
			})
		case "Sung twice":
			data.SongsToReinforce = append(data.SongsToReinforce, views.SuggestionView{
				CandidateSongView: candidate,
				RelationshipLabel: "Sung twice - almost there",
			})
		}
	}

	return data
}

// candidateLookups bundles the per-organization/per-set lookups needed to
// compute one not-yet-added song's rich CandidateSongView (see
// candidateSongView below) - shared by buildSetBuilderData's
// SongsToReinforce pass above and BuildCandidatesSearch's "Browse all
// songs" search endpoint further down, the only two places that still need
// this per-song computation now that the full candidate library isn't
// rendered unconditionally on every page load.
type candidateLookups struct {
	titles             map[string]string
	rolesBySong        map[string][]scheduling.SongRole
	roleTypeLabels     map[string]string
	tagsBySong         map[string][]scheduling.SongTag
	tagCategoryLabels  map[string]string
	candidateConflicts map[string][]string
	repeated           map[string]bool
	trackedNew         map[string]bool
	appearanceCounts   map[string]int
	arrangementCache   map[string]scheduling.SongArrangementInfo
}

// candidateSongView computes song's full CandidateSongView from l's
// already-fetched lookups - the exact per-song computation
// buildSetBuilderData's Candidates loop used to run unconditionally for the
// whole library on every render, factored out so BuildCandidatesSearch can
// run it too, but only for songs that actually match a search (see that
// handler's doc comment).
func (l candidateLookups) candidateSongView(song pco.SongData) views.CandidateSongView {
	roleViews, rolesAttr := songRoleViews(l.rolesBySong, l.roleTypeLabels, song.ID)
	tagViews, tagsAttr := songTagViews(l.tagsBySong, l.tagCategoryLabels, song.ID)

	var conflictTitles []string
	for _, conflictSongID := range l.candidateConflicts[song.ID] {
		conflictTitles = append(conflictTitles, titleOrID(l.titles, conflictSongID))
	}

	// Cache-only, deliberately no live-fetch fallback here (unlike
	// resolveArrangementInfo for ServiceOrder) - Planning Center has no bulk
	// "arrangements for every song" call, so doing this for the whole
	// candidate library would cost one request per song. An uncached
	// candidate just shows nothing until it's either added to a set once
	// (which does the live fetch/backfill) or covered by a Song Info sync.
	var arrangementInfo string
	if info, ok := l.arrangementCache[song.ID]; ok {
		arrangementInfo = formatArrangementInfo(info)
	}

	return views.CandidateSongView{
		SongID:           song.ID,
		Title:            song.Attributes.Title,
		ConflictTitles:   conflictTitles,
		RecentlyRepeated: l.repeated[song.ID],
		NewSongLabel:     reinforcementLabel(l.trackedNew, l.appearanceCounts, song.ID),
		NotSungInLabel:   notSungInLabel(song.Attributes.LastScheduledAt, l.tagsBySong[song.ID]),
		ArrangementInfo:  arrangementInfo,
		Roles:            roleViews,
		RolesAttr:        rolesAttr,
		Tags:             tagViews,
		TagsAttr:         tagsAttr,
	}
}

// buildBrowsableSongs is candidateSongView's lightweight counterpart -
// title-only, for the "Browse all songs" list's empty-query-and-role state
// (see views.SetBuilderData.BrowsableSongs) rather than the rich search
// results the same list shows once someone actually searches. Deliberately
// skips every per-song lookup candidateSongView needs (roles, tags,
// conflicts, arrangement info, reinforcement status) - none of that is
// wanted here, so this is just an id+title filter over songs, cheap enough
// to run on every render unconditionally. songs is expected already
// title-ordered (fetchActiveSongs' own contract) - this doesn't re-sort.
func buildBrowsableSongs(songs []pco.SongData, inSet map[string]bool) []views.SongOptionView {
	browsable := make([]views.SongOptionView, 0, len(songs))
	for _, song := range songs {
		if inSet[song.ID] {
			continue
		}
		browsable = append(browsable, views.SongOptionView{SongID: song.ID, Title: song.Attributes.Title})
	}
	return browsable
}

// rosterPersonOptions turns this plan's roster person ids into the
// lightweight id+name options the song leader picker's <select> needs
// (see SetBuilderData.RosterPersonOptions) - deduplicated (the same
// person can be assigned to more than one position on a roster) and
// sorted by name so the list reads naturally rather than in whatever
// order the roster assignments happen to be stored in.
func rosterPersonOptions(personIDs []string, names map[string]string) []views.RosterPersonOptionView {
	seen := make(map[string]bool, len(personIDs))
	opts := make([]views.RosterPersonOptionView, 0, len(personIDs))
	for _, id := range personIDs {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		opts = append(opts, views.RosterPersonOptionView{PersonID: id, Name: titleOrID(names, id)})
	}
	sort.Slice(opts, func(i, j int) bool { return opts[i].Name < opts[j].Name })
	return opts
}

// buildCandidateLookups gathers everything candidateLookups needs to
// compute a not-yet-added song's rich CandidateSongView - factored out of
// searchCandidates so BuildCandidateDetails (the "Browse all songs" plain
// title list's click-to-expand detail popover, see that handler's doc
// comment) can run the exact same lookups for a single song instead of
// re-deriving or duplicating them. None of these are PCO calls made
// per-song - they're bulk, org-scoped queries (local DB plus the
// already-60s-cached fetchActiveSongs/fetchRecentPlanHistory), so running
// this for one song costs the same handful of queries as running it for a
// whole search's worth of matches.
func (sc *SchedulingController) buildCandidateLookups(ctx context.Context, organizationID, serviceTypeID string, songs []pco.SongData, setID int64) candidateLookups {
	titles := songTitleMap(songs)

	roleTypes, _ := sc.store.ListSongRoleTypes(ctx)
	roleTypeLabels := songRoleTypeLabelMap(roleTypes)
	allRoles, _ := sc.store.ListSongRoles(ctx, organizationID, "", "")
	rolesBySong := songRolesBySong(allRoles)

	allTags, _ := sc.store.ListSongTags(ctx, organizationID, "", "")
	tagCategories, _ := sc.store.ListSongTagCategories(ctx)
	tagsBySong := songTagsBySong(allTags)
	tagCategoryLabels := songTagCategoryLabelMap(tagCategories)

	trackedNew, _ := sc.store.TrackedNewSongs(ctx, organizationID)
	appearanceCounts, _ := sc.store.PublishedSongAppearanceCounts(ctx, organizationID)
	arrangementCache, _ := sc.store.SongArrangementInfo(ctx, organizationID)

	candidateConflicts, _, _ := sc.store.AvoidPairings(ctx, organizationID, setID)
	history := fetchRecentPlanHistory(ctx, serviceTypeID)

	return candidateLookups{
		titles:             titles,
		rolesBySong:        rolesBySong,
		roleTypeLabels:     roleTypeLabels,
		tagsBySong:         tagsBySong,
		tagCategoryLabels:  tagCategoryLabels,
		candidateConflicts: candidateConflicts,
		repeated:           history.RepeatedSongs,
		trackedNew:         trackedNew,
		appearanceCounts:   appearanceCounts,
		arrangementCache:   arrangementCache,
	}
}

// BuildCandidateDetails backs the "Browse all songs" plain title list's
// click-to-expand detail popover (CandidateSearchResults' query=="" state,
// see set_builder.templ's songBrowseRow) - lazy-fetched the first time a
// song's title is clicked there, rather than computing this for the whole
// library up front the same way searchCandidates avoids that cost for the
// rich search results (see that function's doc comment). Renders exactly
// the same songCandidateMeta badge/meta cluster the rich search rows and
// suggestion lists already show, just for one song on demand - clicking a
// title used to instantly add the song; that's now a separate "+" button
// (see songBrowseRow), and the title click reveals this instead.
//
// Unlike candidateSongView's general-purpose arrangement-info lookup
// (cache-only, no fetch - see that method's own doc comment on why: a
// broad search matching up to candidateSearchLimit songs would otherwise
// enqueue a background sync per match), this handler is exactly one song
// per click - safe to resolve through resolveArrangementInfo instead,
// the same enqueue-on-miss path the main service order already uses (see
// that function/resolveArrangementCacheInfo's own doc comments): a cache
// hit returns the same value candidateSongView already computed; a miss
// enqueues a background fetch (deferred triggerSongInfoQueueSync below
// actually starts draining it) so the *next* click on this same song
// shows it filled in, rather than staying blank forever the way a plain
// cache-only lookup would.
func (sc *SchedulingController) BuildCandidateDetails(c *fiber.Ctx) error {
	organizationID := middleware.CurrentOrganizationID(c)
	ctx := middleware.RequestContext(c)
	defer triggerSongInfoQueueSync(ctx, sc.store, organizationID)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")
	songID := c.Params("song_id")

	set, err := sc.store.GetOrCreateSet(ctx, organizationID, serviceTypeID, planID)
	if err != nil {
		return render(c, views.CandidateDetails(views.CandidateSongView{}, "Couldn't load this song's details."))
	}

	songs, _ := fetchActiveSongs(ctx, organizationID)
	var song *pco.SongData
	for i := range songs {
		if songs[i].ID == songID {
			song = &songs[i]
			break
		}
	}
	if song == nil {
		return render(c, views.CandidateDetails(views.CandidateSongView{}, "Song not found."))
	}

	lookups := sc.buildCandidateLookups(ctx, organizationID, serviceTypeID, songs, set.ID)
	cand := lookups.candidateSongView(*song)
	cand.ArrangementInfo = resolveArrangementInfo(ctx, sc.store, organizationID, lookups.arrangementCache, songID)
	return render(c, views.CandidateDetails(cand, ""))
}

// candidateSearchLimit caps BuildCandidatesSearch's response - a broad or
// even empty-role/broad-text query still only ever renders this many rich
// rows, never hundreds, no matter how large the underlying library is (see
// that handler's own doc comment).
const candidateSearchLimit = 50

// BuildCandidatesSearch backs the set builder's "Browse all songs" search
// box and role-filter buttons (set_builder.templ) - the search-driven
// replacement for what buildSetBuilderData's Candidates loop used to
// compute unconditionally, for the entire not-yet-added song library, on
// every single Build page render regardless of whether this collapsible
// section was even open (a real church library can run to hundreds of
// songs - see docs/pco-api-risks.md). Same hx-target-a-narrow-fragment shape as
// BuildPlansSearch (build.go): this renders only
// views.CandidateSearchResults, the <ul id="set-candidates">'s inner
// content, never the whole page or even the whole builder body - so typing
// here never touches (or loses focus in) anything else on the page.
//
// q/role come straight from the query string (search text and the
// currently-active role-filter button's code - see toggleRoleFilter,
// scheduling.js); "" for either means "no filter on that axis." With
// neither set, searchCandidates returns early with just the lightweight
// browsable title list (see its own doc comment) rather than the rich
// candidateSongView treatment - not computing anything expensive until
// someone actually searches is the entire point of this being
// server-driven instead of eagerly rendering everything (see
// CandidateSearchResults' own doc comment for the states it can render).
// Matches are capped at candidateSearchLimit; the results view shows a
// "showing top N of M" hint whenever that cap is actually hit.
func (sc *SchedulingController) BuildCandidatesSearch(c *fiber.Ctx) error {
	organizationID := middleware.CurrentOrganizationID(c)
	ctx := middleware.RequestContext(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")
	query := c.Query("q")
	role := c.Query("role")

	candidates, totalMatches, capped, browsable := sc.searchCandidates(ctx, organizationID, serviceTypeID, planID, query, role)
	return render(c, views.CandidateSearchResults(serviceTypeID, planID, candidates, query, role, totalMatches, capped, browsable, ""))
}

// ArchiveSong flips a song's PCO `hidden` attribute to true from the
// "Browse all songs" candidate list (see
// docs/plans/shipped/song-archive-plan.md) - a real, immediate write
// to the org's live PCO data, confirmed client-side via hx-confirm before
// this ever fires (matching songDetailCard's Duplicate/Remove/Replace
// precedent), never a local draft. No local table has a real FK to a song
// id (song_tags/song_person_preferences/song_relationships/song_roles all
// store a plain, unconstrained song_id), so there is nothing else to
// reconcile - archiving just makes the song stop appearing in
// FetchActiveSongs-backed lists, via InvalidateActiveSongsCache below.
// Re-renders the same #set-candidates fragment BuildCandidatesSearch
// renders, using whatever query/role the candidate list currently has
// live (see the hx-include on the archive button in songCandidateMeta's
// sibling markup) so the archived song simply disappears from the list
// the click came from, same UX as clicking Add.
func (sc *SchedulingController) ArchiveSong(c *fiber.Ctx) error {
	organizationID := middleware.CurrentOrganizationID(c)
	ctx := middleware.RequestContext(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")
	songID := c.Params("song_id")
	query := c.FormValue("q")
	role := c.FormValue("role")

	errMsg := ""
	hidden := true
	if _, err := pco.UpdateSong(ctx, songID, &pco.UpdateSongParams{Hidden: &hidden}); err != nil {
		logger.ErrorContext(ctx, "archive song", "error", err, "song_id", songID)
		errMsg = "Couldn't archive that song in Planning Center: " + err.Error()
	} else {
		scheduling.InvalidateActiveSongsCache(organizationID)
	}

	candidates, totalMatches, capped, browsable := sc.searchCandidates(ctx, organizationID, serviceTypeID, planID, query, role)
	return render(c, views.CandidateSearchResults(serviceTypeID, planID, candidates, query, role, totalMatches, capped, browsable, errMsg))
}

// searchCandidates is BuildCandidatesSearch's actual work, split out so the
// pure matching/capping logic below (candidateMatchesSearch/
// candidateMatchesRole/capCandidates) can be unit tested without a live
// PCO/DB round trip. With query and role both empty, this returns early
// with just browsable (buildBrowsableSongs' title-only list, for the
// "browse titles" empty state - see views.SetBuilderData.BrowsableSongs)
// and zero results, rather than falling through to the rest of this
// function: every lookup below this point (roles, tags, conflicts,
// arrangement cache, repeat history, reinforcement tracking) exists only to
// build the rich CandidateSongView results/candidateSongView is meant to
// compute solely for matches, and with nothing to match against, every song
// in the library would "match" and get that full treatment for nothing -
// exactly the whole-library-on-every-render cost this endpoint exists to
// avoid. Past that early return, fetches its own copy of the same lookups
// buildSetBuilderData gathers for its SongsToReinforce pass - cheaper than
// it sounds despite the duplication: none of buildSetBuilderData's
// plan-item/order-of-service fetches (GetPlan, GetItems, note categories)
// are needed here, fetchActiveSongs itself is the same 60s-cached call
// every other Build page hit already reuses (see docs/pco-api-risks.md),
// and - the actual point of this endpoint existing - only the songs that
// actually match query/role ever get the full candidateSongView treatment,
// capped at candidateSearchLimit, rather than the whole library.
// Best-effort throughout, same convention as buildSetBuilderData: a failed
// lookup just degrades whatever it backs (e.g. no conflict badges) instead
// of failing the search outright.
func (sc *SchedulingController) searchCandidates(ctx context.Context, organizationID, serviceTypeID, planID, query, role string) (results []views.CandidateSongView, totalMatches int, capped bool, browsable []views.SongOptionView) {
	set, err := sc.store.GetOrCreateSet(ctx, organizationID, serviceTypeID, planID)
	if err != nil {
		return nil, 0, false, nil
	}
	setSongs, err := sc.store.ListSetSongs(ctx, set.ID)
	if err != nil {
		return nil, 0, false, nil
	}
	inSet := make(map[string]bool, len(setSongs))
	for _, ss := range setSongs {
		inSet[ss.SongID] = true
	}

	songs, _ := fetchActiveSongs(ctx, organizationID)

	if query == "" && role == "" {
		return nil, 0, false, buildBrowsableSongs(songs, inSet)
	}

	lookups := sc.buildCandidateLookups(ctx, organizationID, serviceTypeID, songs, set.ID)

	var matches []views.CandidateSongView
	for _, song := range songs {
		if inSet[song.ID] {
			continue
		}
		// Matching happens against the same cheap attrs (rolesAttr/
		// tagsAttr) the old client-side filterCandidates used - computed
		// here just to test the filter, and again inside
		// candidateSongView for the songs that actually pass it (a second,
		// identical map lookup - simpler than threading them through, and
		// negligible next to everything else this loop already does per
		// song).
		_, rolesAttr := songRoleViews(lookups.rolesBySong, lookups.roleTypeLabels, song.ID)
		if !candidateMatchesRole(rolesAttr, role) {
			continue
		}
		_, tagsAttr := songTagViews(lookups.tagsBySong, lookups.tagCategoryLabels, song.ID)
		if !candidateMatchesSearch(song.Attributes.Title, tagsAttr, query) {
			continue
		}
		matches = append(matches, lookups.candidateSongView(song))
	}

	totalMatches = len(matches)
	results, capped = capCandidates(matches, candidateSearchLimit)
	return results, totalMatches, capped, nil
}

// candidateMatchesSearch reports whether a not-yet-added song matches a
// typed search term against its title or tag values (tagsAttr is already
// lowercased - see songTagViews), case-insensitive substring - the same two
// fields the old client-side filterCandidates matched against. "" (nothing
// typed) matches everything.
func candidateMatchesSearch(title, tagsAttr, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	haystack := strings.ToLower(title) + " " + tagsAttr
	return strings.Contains(haystack, query)
}

// candidateMatchesRole reports whether role (the active role-filter
// button's code, "" when none is active) is one of rolesAttr's
// space-separated codes - "" matches everything, same as
// candidateMatchesSearch.
func candidateMatchesRole(rolesAttr, role string) bool {
	if role == "" {
		return true
	}
	return strings.Contains(" "+rolesAttr+" ", " "+role+" ")
}

// capCandidates trims matches down to at most limit rows, reporting
// whether it actually had to (see views.CandidateSearchResults' "showing
// top N of M" hint) - split out from searchCandidates so this small but
// easy-to-get-backwards slicing has its own direct test.
func capCandidates(matches []views.CandidateSongView, limit int) (capped []views.CandidateSongView, wasCapped bool) {
	if len(matches) <= limit {
		return matches, false
	}
	return matches[:limit], true
}

// reinforcementLabel returns the short badge text for a song that's
// genuinely new to the church (see tracked_new_songs/TrackNewSong) and
// hasn't been sung enough times yet to graduate out of reinforcement - ""
// for anything else (not tracked as new, never actually published yet, or
// already sung 3+ times). See SetBuilderData.SongsToReinforce and
// CandidateSongView.NewSongLabel, which this drives.
func reinforcementLabel(tracked map[string]bool, counts map[string]int, songID string) string {
	if !tracked[songID] {
		return ""
	}
	switch counts[songID] {
	case 1:
		return "Sung once"
	case 2:
		return "Sung twice"
	default:
		return ""
	}
}

// notSungInThreshold is how long since a song's PCO LastScheduledAt before
// the "not sung in a while" nudge kicks in - a real, concrete default
// (180 days, ~6 months), not an org-level setting in v1. Simple on
// purpose: an org-configurable threshold could be added later if
// requested, but isn't built here.
const notSungInThreshold = 180 * 24 * time.Hour

// notSungInLabel returns the short badge text for a song that used to get
// scheduled regularly but has gone cold - "" for anything else. A song
// with no LastScheduledAt at all (never scheduled, or PCO has no history
// that far back) doesn't qualify - that's TrackNewSong's reinforcement
// path instead (see reinforcementLabel above), not this one. A song
// carrying any tag in scheduling.TagCategorySeasonal is exempted
// regardless of how long it's been - an intentional gap (Christmas,
// Easter, ...) is by design, not a signal. See
// CandidateSongView.NotSungInLabel, which this drives.
func notSungInLabel(lastScheduledAt *time.Time, tags []scheduling.SongTag) string {
	if lastScheduledAt == nil {
		return ""
	}
	if time.Since(*lastScheduledAt) < notSungInThreshold {
		return ""
	}
	for _, tag := range tags {
		if tag.Category == scheduling.TagCategorySeasonal {
			return ""
		}
	}
	return "Not sung recently"
}

// recentTwoPlanLookback is how many of the most recent past plans
// fetchRecentPlanHistory compares - "the two sets before it", per the
// repeat-warning feature's own description.
const recentTwoPlanLookback = 2

// recentPlanHistory bundles what buildSetBuilderData needs from a service
// type's two most recent past plans: which songs repeated across both of
// them (RepeatedSongs - what recentlyRepeatedSongs used to return on its
// own) and their average total length (AverageLength), for the
// service-length warning below. One GetPlans/GetItems round trip covers
// both, rather than fetching the same two plans' items twice.
type recentPlanHistory struct {
	RepeatedSongs map[string]bool
	// AverageLength is the mean total item length (seconds) across the two
	// plans compared - 0 when there weren't two past plans to compare,
	// which serviceLengthWarning treats as "nothing to compare against"
	// rather than "this plan is infinitely long."
	AverageLength int
}

// recentPlanHistoryCacheTTL bounds how stale a cached recentPlanHistory can
// be - same value and reasoning as scheduling.FetchActiveSongs'
// songCacheTTL (see pco_songs.go): the two most recent *past* plans for a
// service type are effectively session-stable (a plan doesn't cross from
// future to past mid-session in practice), so a minute of staleness costs
// nothing, and this exists purely to stop re-fetching/re-summing the same
// two plans' items on every full builder render (add/remove/publish/
// discard already re-triggers buildSetBuilderData from scratch) and now,
// since BuildCandidatesSearch also calls this, on every debounced
// candidate search too - each call was otherwise a live GetPlans + 2x
// GetItems, uncached, exactly the kind of hot-path PCO call CLAUDE.md's
// own "PCO API calls" section says to cache when the underlying data is
// session-stable like this.
const recentPlanHistoryCacheTTL = 60 * time.Second

// cachedRecentPlanHistory is one service type's last-computed
// recentPlanHistory, plus when it was computed.
type cachedRecentPlanHistory struct {
	history  recentPlanHistory
	cachedAt time.Time
}

var (
	recentPlanHistoryCacheMu sync.Mutex
	recentPlanHistoryCache   = map[string]cachedRecentPlanHistory{}
)

// fetchRecentPlanHistory returns serviceTypeID's recent-plan history,
// straight from Planning Center's own item data - not this tool's local
// sets, since the point of both signals it backs is to compare against
// what actually got sung/ran, regardless of whether it was built here.
// Cached per service type (see recentPlanHistoryCacheTTL) - a PCO service
// type id is already org-scoped (it belongs to exactly one PCO account),
// so no separate organization key is needed, mirroring FetchActiveSongs'
// own per-organization cache one level up. A transient PCO failure is
// never cached (see fetchRecentPlanHistoryFromPCO's ok return) so it can't
// pin a stale/empty result in for the rest of the window - same fail-safe
// convention as FetchActiveSongs. The actual overlap computation lives in
// songsInBoth so it can be unit tested without hitting Planning Center.
func fetchRecentPlanHistory(ctx context.Context, serviceTypeID string) recentPlanHistory {
	now := time.Now()

	recentPlanHistoryCacheMu.Lock()
	entry, ok := recentPlanHistoryCache[serviceTypeID]
	recentPlanHistoryCacheMu.Unlock()
	if ok && now.Sub(entry.cachedAt) < recentPlanHistoryCacheTTL {
		return entry.history
	}

	history, fetchOK := fetchRecentPlanHistoryFromPCO(ctx, serviceTypeID)
	if fetchOK {
		recentPlanHistoryCacheMu.Lock()
		recentPlanHistoryCache[serviceTypeID] = cachedRecentPlanHistory{history: history, cachedAt: now}
		recentPlanHistoryCacheMu.Unlock()
	}
	return history
}

// fetchRecentPlanHistoryFromPCO does fetchRecentPlanHistory's actual live
// work. ok is false only for a genuine PCO call failure (not cache-worthy -
// might well succeed a moment later); fewer than two past plans to compare
// is a legitimate, stable answer (ok=true, zero-value recentPlanHistory),
// same as any other successful-but-empty result - serviceLengthWarning
// already treats AverageLength==0 as "nothing to compare against" rather
// than "this plan is infinitely long," so callers see no behavior
// difference from before this had a cache at all.
func fetchRecentPlanHistoryFromPCO(ctx context.Context, serviceTypeID string) (history recentPlanHistory, ok bool) {
	plans, err := pco.GetPlans(ctx, serviceTypeID, &pco.PlansParams{
		Filter:  "past",
		OrderBy: "-sort_date",
		PerPage: recentTwoPlanLookback,
	})
	if err != nil {
		return recentPlanHistory{}, false
	}
	if len(plans.Data) < recentTwoPlanLookback {
		return recentPlanHistory{}, true
	}

	items, err := pco.GetItems(ctx, serviceTypeID, plans.Data[0].ID, nil)
	if err != nil {
		return recentPlanHistory{}, false
	}
	prevItems, err := pco.GetItems(ctx, serviceTypeID, plans.Data[1].ID, nil)
	if err != nil {
		return recentPlanHistory{}, false
	}

	return recentPlanHistory{
		RepeatedSongs: songsInBoth(items.Data, prevItems.Data),
		AverageLength: (totalItemLength(items.Data) + totalItemLength(prevItems.Data)) / recentTwoPlanLookback,
	}, true
}

// totalItemLength sums Length (seconds, PCO's own unit for the attribute)
// across a plan's raw items - the past-plan side of the service-length
// comparison; see serviceOrderLengthSeconds for the current draft's side of
// the same comparison.
func totalItemLength(items []pco.ItemData) int {
	total := 0
	for _, item := range items {
		total += item.Attributes.Length
	}
	return total
}

// songsInBoth returns the song IDs that appear (via a song-linked item) in
// both given plans' item lists.
func songsInBoth(a, b []pco.ItemData) map[string]bool {
	songsIn := func(items []pco.ItemData) map[string]bool {
		songs := make(map[string]bool)
		for _, item := range items {
			if item.Relationships.Song.Data != nil {
				songs[item.Relationships.Song.Data.ID] = true
			}
		}
		return songs
	}

	aSongs, bSongs := songsIn(a), songsIn(b)

	both := make(map[string]bool)
	for songID := range aSongs {
		if bSongs[songID] {
			both[songID] = true
		}
	}
	return both
}

// mergedRow is one row of a plan's full order once this set's songs are
// interleaved into it - exactly one of Item/Song is set, never both.
type mergedRow struct {
	Item *pco.ItemData
	Song *scheduling.SetSong
}

// deriveImportRows is RevertSet's core: given a plan's items (already
// sorted by sequence), it rebuilds what the local set_songs rows should be
// to exactly mirror what's live - the reverse of mergeServiceOrder's walk.
// Each song-linked item becomes a row anchored to whatever non-song (or
// non-song-linked) item most recently preceded it; consecutive songs with
// nothing foreign between them share that same anchor, ordered by Position
// in the order they appear.
//
// KeyOverride is seeded from the item's own live KeyName rather than left
// empty - without this, a freshly-reverted row falls through effectiveKey
// to song_arrangement_cache's single org-wide cached key for that song,
// which can easily be a different arrangement's key than the one this
// specific item actually has (a song with more than one Planning Center
// arrangement, or an item whose key was set by hand) - confirmed live: a
// plan with keys "B A A A" imported as "B A Bb A" because the third item's
// real key never got read at all. Real, not "" cleared-on-purpose, so this
// only shows up as InfoPending on a genuine later edit (UpdateSetSong
// KeyOverride), never on the import itself.
func deriveImportRows(items []pco.ItemData) []scheduling.SetSong {
	var rows []scheduling.SetSong
	anchor := ""
	for _, item := range items {
		if item.Attributes.ItemType == pco.ItemTypeSong && item.Relationships.Song.Data != nil {
			rows = append(rows, scheduling.SetSong{
				SongID:       item.Relationships.Song.Data.ID,
				Position:     len(rows),
				PCOItemID:    item.ID,
				AnchorItemID: anchor,
				KeyOverride:  item.Attributes.KeyName,
			})
			continue
		}
		anchor = item.ID
	}
	return rows
}

// trackedPCOItemIDs is every PCO item id already spoken for by this set's
// local rows, active or tombstoned - shared by MergeSet (to know what NOT
// to re-import) and buildSetBuilderData's own MergeAvailableCount below (to
// know whether MergeSet would find anything at all). A tombstoned row still
// counts: it was deliberately removed here, so re-importing it out from
// under that removal would undo a real local decision, not just fill a gap.
func trackedPCOItemIDs(setSongs, pendingDeletions []scheduling.SetSong) map[string]bool {
	tracked := make(map[string]bool, len(setSongs)+len(pendingDeletions))
	for _, ss := range setSongs {
		if ss.PCOItemID != "" {
			tracked[ss.PCOItemID] = true
		}
	}
	for _, ss := range pendingDeletions {
		if ss.PCOItemID != "" {
			tracked[ss.PCOItemID] = true
		}
	}
	return tracked
}

// pcoItemsFingerprint returns a deterministic fingerprint of a plan's live
// items - the poll-while-open live-sync check's comparison value (see
// SetBuilderData.PCOSyncFingerprint and SyncCheck, sets_publish.go; design
// in docs/plans/in-progress/live-sync-reflection-plan.md). Two
// fingerprints are equal iff the same set of item ids exist with the same
// Sequence and UpdatedAt values each - which covers every kind of change
// the poll needs to catch:
//   - an add/remove (the set of ids itself changes);
//   - a reorder (Sequence) - live-verified against the dev PCO org that a
//     pure pco.ReorderItems call does NOT bump the moved items' own
//     UpdatedAt, so Sequence has to be included explicitly rather than
//     inferred from UpdatedAt alone;
//   - any attribute edit, title/description/key included (UpdatedAt) -
//     live-verified that Item.UpdatedAt does bump on any such edit, unlike
//     Plan.UpdatedAt, which doesn't reflect item-level changes at all (see
//     docs/pco-api-risks.md) - which is why this fingerprints items, never
//     the plan itself.
//
// Sorted by ID first so the order items happen to come back from the API
// in never affects the result - only Sequence itself is meant to encode
// order.
func pcoItemsFingerprint(items []pco.ItemData) string {
	sorted := make([]pco.ItemData, len(items))
	copy(sorted, items)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })

	var b strings.Builder
	for _, item := range sorted {
		fmt.Fprintf(&b, "%s:%d:%d|", item.ID, item.Attributes.Sequence, item.Attributes.UpdatedAt.Unix())
	}

	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// deriveMergeImportRows is MergeSet's core: given a plan's items (already
// sorted by sequence) and the set of PCO item ids already tracked locally
// (every existing set_songs row for this set, active or tombstoned, that
// has a PCOItemID - see MergeSet), it returns just the song-linked items
// that aren't tracked yet - songs added directly in Planning Center since
// the local draft was last touched. Each returned row is anchored to
// whichever tracked or foreign item most recently preceded it in the walk
// (mirroring deriveImportRows), so mergeServiceOrder can place it
// correctly relative to what's already there; Position/PCOItemID are left
// for the caller (Store.ImportNewSongsFromPCO) to fill in, since where a
// new row actually lands within its anchor group depends on what's already
// occupying it locally, not just this walk. Unlike deriveImportRows, the
// anchor is advanced on every item (tracked or not) rather than only on
// non-song items - a tracked song can sit between two new ones, and the
// second must anchor to the first, not to whatever foreign item came
// before both.
//
// KeyOverride is seeded from the item's own live KeyName, same reasoning
// as deriveImportRows above - a merged-in song is exactly as likely to sit
// on a non-default arrangement (or a hand-set key) as one pulled in by a
// full revert.
func deriveMergeImportRows(items []pco.ItemData, trackedItemIDs map[string]bool) []scheduling.SetSong {
	var rows []scheduling.SetSong
	anchor := ""
	for _, item := range items {
		if item.Attributes.ItemType == pco.ItemTypeSong && item.Relationships.Song.Data != nil {
			if !trackedItemIDs[item.ID] {
				rows = append(rows, scheduling.SetSong{
					SongID:       item.Relationships.Song.Data.ID,
					PCOItemID:    item.ID,
					AnchorItemID: anchor,
					KeyOverride:  item.Attributes.KeyName,
				})
			}
			anchor = item.ID
			continue
		}
		anchor = item.ID
	}
	return rows
}

// mergeServiceOrder interleaves a plan's existing items (already sorted by
// sequence) with a set's songs, each song placed immediately after its
// anchor (SetSong.AnchorItemID), grouped there in local position order;
// songs anchored to "" (top of service) come first, before any existing
// item. An item that's one of the set's own tracked songs never gets its
// own separate row here (it renders via its song row instead, not twice),
// but its group is still flushed at its position in the walk - normally a
// no-op (a set song's anchor is never another set song's own PCOItemID, by
// construction in AddSongToSet/RepositionSongInSet), except for a row
// MergeSet (sets_publish.go) imported anchored to an already-tracked item,
// which does rely on this to land right after that item rather than at the
// very end as a stale anchor. A song anchored to an item id that isn't in
// items - e.g. that item was deleted in PCO since - is appended at the
// very end rather than silently dropped.
func mergeServiceOrder(items []pco.ItemData, setSongs []scheduling.SetSong) []mergedRow {
	// Keyed by PCO item id, not song id: a song can now appear more than
	// once in the set (see DuplicateSongInSet), so "this song is tracked"
	// no longer implies "every PCO item linked to it is ours" - only the
	// specific items our own rows were published as (PCOItemID) are.
	trackedItemIDs := make(map[string]bool, len(setSongs))
	for _, ss := range setSongs {
		if ss.PCOItemID != "" {
			trackedItemIDs[ss.PCOItemID] = true
		}
	}

	byAnchor := map[string][]scheduling.SetSong{}
	for _, ss := range setSongs {
		byAnchor[ss.AnchorItemID] = append(byAnchor[ss.AnchorItemID], ss)
	}
	for anchor := range byAnchor {
		group := byAnchor[anchor]
		sort.Slice(group, func(i, j int) bool { return group[i].Position < group[j].Position })
	}

	appendGroup := func(rows []mergedRow, anchor string) []mergedRow {
		for _, ss := range byAnchor[anchor] {
			ss := ss
			rows = append(rows, mergedRow{Song: &ss})
		}
		delete(byAnchor, anchor)
		return rows
	}

	var rows []mergedRow
	rows = appendGroup(rows, "")

	for _, item := range items {
		if !trackedItemIDs[item.ID] {
			item := item
			rows = append(rows, mergedRow{Item: &item})
		}
		rows = appendGroup(rows, item.ID)
	}

	// Leftover groups: anchors that didn't match any current item. Sorted
	// just for determinism, not because the anchor value means anything
	// once orphaned.
	var stale []string
	for anchor := range byAnchor {
		stale = append(stale, anchor)
	}
	sort.Strings(stale)
	for _, anchor := range stale {
		rows = appendGroup(rows, anchor)
	}

	return rows
}

// serviceLengthWarnRatio and serviceLengthWarnFloorSeconds together decide
// how far a draft's total length has to differ from this service type's
// recent average before it's "noticeably" long/short rather than normal
// week-to-week variance: 15% of the average, or 3 minutes, whichever is
// larger - the floor exists so a service type with a small average (a short
// midweek gathering, say) doesn't flag on a variance of a minute or two
// that 15% alone would call "noticeable."
const (
	serviceLengthWarnRatio        = 0.15
	serviceLengthWarnFloorSeconds = 180
)

// serviceOrderLengthSeconds sums this draft's total length: a foreign
// item's own Length attribute (PCO tracks this for any item type, not just
// songs - a sermon or video counts too), or a set song's cached arrangement
// length (song_arrangement_cache.LengthSeconds, the same figure
// ArrangementInfo's summary line already displays as "3:45"). An uncached
// song contributes 0 - same best-effort convention as ArrangementInfo
// elsewhere on this page: the total undercounts until Song Info is synced,
// rather than the page breaking.
func serviceOrderLengthSeconds(rows []mergedRow, arrangementCache map[string]scheduling.SongArrangementInfo) int {
	total := 0
	for _, row := range rows {
		if row.Item != nil {
			total += row.Item.Attributes.Length
			continue
		}
		total += arrangementCache[row.Song.SongID].LengthSeconds
	}
	return total
}

// serviceLengthWarning compares this draft's total length against this
// service type's recent average (see fetchRecentPlanHistory) and returns an
// advisory message when they differ by more than serviceLengthWarnRatio/
// serviceLengthWarnFloorSeconds - "" when there's no history to compare
// against yet, or the difference is within normal variance.
func serviceLengthWarning(currentSeconds, averageSeconds int) string {
	if averageSeconds <= 0 {
		return ""
	}

	diff := currentSeconds - averageSeconds
	if diff < 0 {
		diff = -diff
	}
	threshold := int(float64(averageSeconds) * serviceLengthWarnRatio)
	if threshold < serviceLengthWarnFloorSeconds {
		threshold = serviceLengthWarnFloorSeconds
	}
	if diff <= threshold {
		return ""
	}

	direction := "longer"
	if currentSeconds < averageSeconds {
		direction = "shorter"
	}
	return fmt.Sprintf(
		"This plan runs about %s, %s than the recent average of %s for this service type - double check before publishing.",
		formatTotalDuration(currentSeconds), direction, formatTotalDuration(averageSeconds),
	)
}

// formatTotalDuration renders a whole-service length ("1h 15m", "48 min") -
// deliberately not the "3:45" m:ss format formatArrangementInfo uses for a
// single song, which stops being readable once the total runs past an
// hour.
func formatTotalDuration(seconds int) string {
	minutes := seconds / 60
	if minutes < 60 {
		return fmt.Sprintf("%d min", minutes)
	}
	return fmt.Sprintf("%dh %dm", minutes/60, minutes%60)
}

// Pitch-class parsing/distance (formerly keyPitchClasses/
// parseKeyPitchClass/semitoneDistance, inlined here for keyTransitionWarnings
// alone) moved to scheduling.ParseKeyPitchClass/scheduling.SemitoneDistance -
// see internal/scheduling/key_engine.go and
// docs/plans/in-progress/vocal-range-plan.md's "Phase 2 prerequisite: the
// key engine" section for why (song vocal range, Phase 2, is a second
// consumer that also needs to transpose a stored interval, not just
// compare two bare keys - that needed a shared, storage-reachable home,
// not another controller-local copy). suggestedDominantSeventh/
// suggestedKeyChangeMove below still call the scheduling. versions.

// dominantSpellings maps a pitch class (0-11) to its conventional spelled
// note name, used by suggestedDominantSeventh below to turn the destination
// key's V7 (dominantPC = (destPC+7)%12) back into a chord name a director
// would actually recognize - see
// docs/plans/shipped/key-change-tip-plan.md's worked table for how each
// entry was derived, including why a naive letter-cycling transposition
// (reject) was replaced by this static table (it produces an absurd
// spelling - "E#7" - for A# major's dominant, where this table says "F7").
// The 7 natural pitch classes (0,2,4,5,7,9,11) have exactly one entry; the
// 5 black-key pitch classes (1,3,6,8,10) are ambiguous (sharp or flat) and
// are resolved by suggestedDominantSeventh matching the destination key's
// own accidental style, not by this table alone.
var dominantSpellings = map[int]string{
	0: "C", 1: "C#", 2: "D", 3: "D#", 4: "E", 5: "F",
	6: "F#", 7: "G", 8: "G#", 9: "A", 10: "A#", 11: "B",
}

// dominantSpellingsFlat is the flat-spelled alternative for the 5 ambiguous
// (black-key) pitch classes only - see dominantSpellings.
var dominantSpellingsFlat = map[int]string{
	1: "Db", 3: "Eb", 6: "Gb", 8: "Ab", 10: "Bb",
}

// suggestedDominantSeventh returns the V7 chord (e.g. "Ab7") to suggest
// playing right before a key change into destKey - a secondary/applied
// dominant, standard functional-harmony practice for pulling strongly into
// a target key regardless of how far away the previous key was (see
// docs/plans/shipped/key-change-tip-plan.md's "The formula and the table").
// destKey must already have parsed via scheduling.ParseKeyPitchClass; this
// only re-derives the pitch class, it doesn't re-validate the string.
// Returns "" if destKey doesn't parse (defensive - callers only reach this
// after scheduling.ParseKeyPitchClass already succeeded on the same
// string).
func suggestedDominantSeventh(destKey string) string {
	destPC, ok := scheduling.ParseKeyPitchClass(destKey)
	if !ok {
		return ""
	}
	dominantPC := (destPC + 7) % 12

	flatName, ambiguous := dominantSpellingsFlat[dominantPC]
	if !ambiguous {
		return dominantSpellings[dominantPC] + "7"
	}

	hasFlat := strings.ContainsAny(destKey, "b♭")
	hasSharp := strings.ContainsAny(destKey, "#♯")
	switch {
	case hasFlat:
		return flatName + "7"
	case hasSharp:
		return dominantSpellings[dominantPC] + "7"
	default:
		// A bare natural-letter destination with an ambiguous dominant and
		// no accidental of its own to match against - only ever reached by
		// "B" (destPC 11, the one natural letter whose dominant, pitch
		// class 6, is one of the 5 ambiguous black keys). Hard-code the
		// real-world/circle-of-fifths answer (F#, never "Gb") rather than
		// guessing generically. A non-canonical bare-flat/bare-sharp
		// spelling like "Cb" (also pitch class 11) hits the hasFlat branch
		// above instead, correctly resolving to its own flat spelling
		// (Gb7) rather than this exception - see the plan doc's "The
		// formula and the table" section.
		return "F#7"
	}
}

// keyChangeTips holds a specific suggested-move override for a semitone
// distance (scheduling.SemitoneDistance), when the generic V7-of-destination default
// (defaultKeyChangeMove) isn't the best available tip for that interval.
// Empty today - neither interval keyTransitionWarnings currently flags
// (1, 6) has a better-known alternative to V7, per
// docs/plans/shipped/key-change-tip-plan.md's music-theory research - but
// the shape exists so a future specific tip is a one-line addition, not a
// refactor: add an entry here, suggestedKeyChangeMove already checks it
// first.
//
// IMPORTANT: adding an entry here alone does nothing for a distance
// keyTransitionWarnings doesn't already flag (e.g. distance 5, a 4th/5th -
// see the plan doc's "Per-interval tips" section on why that one's a
// strong future candidate). That function's own `change != 1 && change !=
// 6` gate has to change too before this map's entry for a new distance is
// ever reached - and widening that gate is a real product-scope decision
// (a "smooth transition" note is a different kind of message than a
// warning), not a data change, so don't add one silently.
var keyChangeTips = map[int]func(destKey string) string{}

// defaultKeyChangeMove is the fallback for any interval without its own
// entry in keyChangeTips above - the V7-of-destination move
// (suggestedDominantSeventh), which the plan doc's research found works
// correctly regardless of interval size, so it's a real answer here, not a
// placeholder standing in for one.
func defaultKeyChangeMove(destKey string) string {
	chord := suggestedDominantSeventh(destKey)
	return fmt.Sprintf("Try hitting %s (the V7 of %s) right before the change - it resolves strongly into the new key.", chord, destKey)
}

// suggestedKeyChangeMove is what keyTransitionWarnings actually calls: an
// interval-specific override if one exists in keyChangeTips, the shared
// default otherwise. See docs/plans/shipped/key-change-tip-plan.md.
func suggestedKeyChangeMove(distance int, destKey string) string {
	if tip, ok := keyChangeTips[distance]; ok {
		return tip(destKey)
	}
	return defaultKeyChangeMove(destKey)
}

// keyTransitionWarning is one flagged key change between two songs that are
// truly back-to-back in the order (see keyTransitionWarnings). SongRowID is
// the set_songs row id (SetSong.ID) of the *second*, later song in the
// pair, not the first - per docs/plans/shipped/in-place-set-warnings-plan.md's
// resolved rendering decision, this renders as a badge on that later song's
// own row (captioned so it clearly reads as being about the transition, not
// a flaw in that song itself), not a separate spacer row between the two.
// Label is the short, always-visible badge text; Message is the full
// sentence, revealed via a click-to-expand components.Disclosure rather
// than shown inline (too long for a badge - see songBadgeSpans,
// set_builder.templ). Suggestion is one more sentence, appended after
// Message in that same popover, suggesting the actual move that eases the
// transition (suggestedKeyChangeMove) - see
// docs/plans/shipped/key-change-tip-plan.md.
type keyTransitionWarning struct {
	SongRowID  int64
	Label      string
	Message    string
	Suggestion string
}

// keyTransitionWarnings flags two songs that are truly back-to-back in the
// order - nothing else between them in rows, see mergeServiceOrder - whose
// keys are a half-step apart: the single interval most likely to sound like
// a mistake rather than an intentional change, and the hardest for a
// guitarist to prepare for (a full re-fret, not a capo move). A song with
// no key set, or a key string that doesn't parse (see
// scheduling.ParseKeyPitchClass), is silently skipped - same best-effort
// convention as fetchRecentPlanHistory above.
func keyTransitionWarnings(rows []mergedRow, arrangementCache map[string]scheduling.SongArrangementInfo, titles map[string]string) []keyTransitionWarning {
	var warnings []keyTransitionWarning
	for i := 1; i < len(rows); i++ {
		prev, cur := rows[i-1], rows[i]
		if prev.Song == nil || cur.Song == nil {
			continue
		}

		prevKey := effectiveKey(*prev.Song, arrangementCache[prev.Song.SongID])
		curKey := effectiveKey(*cur.Song, arrangementCache[cur.Song.SongID])
		prevPitch, ok := scheduling.ParseKeyPitchClass(prevKey)
		if !ok {
			continue
		}
		curPitch, ok := scheduling.ParseKeyPitchClass(curKey)
		if !ok {
			continue
		}
		change := scheduling.SemitoneDistance(prevPitch, curPitch)
		// minor second or tri tone
		if change != 1 && change != 6 {
			continue
		}

		kind, label := "half-step", "half-step from previous song"
		if change == 6 {
			kind, label = "tri-tone", "tritone from previous song"
		}
		warnings = append(warnings, keyTransitionWarning{
			SongRowID: cur.Song.ID,
			Label:     label,
			Message: fmt.Sprintf(
				"%s (%s) into %s (%s) is a %s key change with no transition - consider a pad/bridge, or adjusting one song's key.",
				titleOrID(titles, prev.Song.SongID), prevKey,
				titleOrID(titles, cur.Song.SongID), curKey,
				kind,
			),
			Suggestion: suggestedKeyChangeMove(change, curKey),
		})
	}
	return warnings
}

// itemNotesFromIncluded extracts item id -> that item's ItemNote list from
// a GetItems response's Included array (fetched with
// Include: []string{"item_notes"}), mirroring teamNamesFromIncluded/
// planSortDatesFromIncluded (rosters_builder.go) - except a sideloaded
// ItemNote is already the exact JSON:API resource shape go-pco's own
// ItemNoteData decodes (type/id/attributes/relationships), so this decodes
// straight into that instead of a bespoke lightweight struct. Any entry
// that isn't a well-formed ItemNote, or has no Item relationship to key by,
// is silently skipped - same best-effort spirit as buildSongNoteViews's
// old per-item GetItemNotes call, which likewise just meant an empty note
// set rather than a broken page.
func itemNotesFromIncluded(included []any) map[string][]pco.ItemNoteData {
	notesByItem := map[string][]pco.ItemNoteData{}
	for _, raw := range included {
		b, err := json.Marshal(raw)
		if err != nil {
			continue
		}
		var note pco.ItemNoteData
		if err := json.Unmarshal(b, &note); err != nil {
			continue
		}
		if note.Type != "ItemNote" || note.ID == "" {
			continue
		}
		itemRef := note.Relationships.Item.Data
		if itemRef == nil || itemRef.ID == "" {
			continue
		}
		notesByItem[itemRef.ID] = append(notesByItem[itemRef.ID], note)
	}
	return notesByItem
}

// itemAssignmentsFromIncluded is itemNotesFromIncluded's counterpart for
// ItemAssignment (PCO's real "Song leader" resource - see
// docs/plans/shipped/song-leader-plan.md's follow-up note and
// syncLeaderAssignment, sets_publish.go/leader_note.go) - extracts item id
// -> that item's ItemAssignment list from a GetItems response's Included
// array (fetched with Include: []string{"item_assignments"}). Same
// best-effort spirit: any entry that isn't a well-formed ItemAssignment, or
// has no Item relationship to key by, is silently skipped rather than
// failing the whole page/publish over one malformed sideload.
func itemAssignmentsFromIncluded(included []any) map[string][]pco.ItemAssignmentData {
	assignmentsByItem := map[string][]pco.ItemAssignmentData{}
	for _, raw := range included {
		b, err := json.Marshal(raw)
		if err != nil {
			continue
		}
		var assignment pco.ItemAssignmentData
		if err := json.Unmarshal(b, &assignment); err != nil {
			continue
		}
		if assignment.Type != "ItemAssignment" || assignment.ID == "" {
			continue
		}
		itemRef := assignment.Relationships.Item.Data
		if itemRef == nil || itemRef.ID == "" {
			continue
		}
		assignmentsByItem[itemRef.ID] = append(assignmentsByItem[itemRef.ID], assignment)
	}
	return assignmentsByItem
}

// buildSongNoteViews resolves one views.SongNoteView per available note
// category for a published song's plan item (itemID) - "" means
// unpublished, and there's no item to attach a note to yet, so this
// returns nil. itemNotes is the whole plan's notes, keyed by item id (see
// itemNotesFromIncluded) - sideloaded once on the GetItems call that also
// fetched itemID itself, so this is now a pure lookup with no PCO call of
// its own.
func buildSongNoteViews(itemID string, noteCategories []pco.ItemNoteCategoryData, itemNotes map[string][]pco.ItemNoteData) []views.SongNoteView {
	if itemID == "" || len(noteCategories) == 0 {
		return nil
	}

	byCategory := make(map[string]pco.ItemNoteData, len(itemNotes[itemID]))
	for _, n := range itemNotes[itemID] {
		if n.Relationships.ItemNoteCategory.Data != nil {
			byCategory[n.Relationships.ItemNoteCategory.Data.ID] = n
		}
	}

	noteViews := make([]views.SongNoteView, 0, len(noteCategories))
	for _, cat := range noteCategories {
		v := views.SongNoteView{CategoryID: cat.ID, CategoryName: cat.Attributes.Name}
		if note, ok := byCategory[cat.ID]; ok {
			v.NoteID = note.ID
			v.Content = note.Attributes.Content
		}
		noteViews = append(noteViews, v)
	}
	return noteViews
}

// renderServiceOrder turns a merge into display rows plus the anchor
// picker's option list (every foreign item, in order, prefixed by "Top of
// service"). noteCategories is this service type's Planning Center note
// categories (Audio/Visual, Band, Vocals, etc.), and itemNotes is every
// item's actual notes (see itemNotesFromIncluded) - both fetched once by
// the caller and reused for every published song's SongNoteView (see
// buildSongNoteViews) - a service type has few note categories, and a
// plan's items/notes are sideloaded on one GetItems call, so there's no
// per-song lookup left here at all.
//
// repeated/conflictTitlesBySong/transitionsByRow are the three per-row
// warning inputs this now attaches directly onto each song's own
// SetSongView (RecentlyRepeated/ConflictTitles/KeyTransitionLabel+Message)
// instead of a separate top-of-page list - see
// docs/plans/shipped/in-place-set-warnings-plan.md. transitionsByRow is
// keyed by the *second* song's own row id (keyTransitionWarning.SongRowID),
// so a row only ever picks up a transition badge for the pair ending at
// itself, never the one starting from it.
//
// leaderNames resolves each SetSong.LeaderPersonID to a display name
// (SetSongView.LeaderPersonName) - see distinctLeaderPersonIDs/
// personNameMapForIDs in buildSetBuilderData, batched once for the whole
// render rather than a per-row lookup.
//
// vocalRangesBySong is Phase 2 of docs/plans/in-progress/vocal-range-plan.md
// (scheduling.ListSongVocalRanges, buildSetBuilderData) - a song with a
// stored range gets its practical sung range transposed into this row's
// own effective key (scheduling.EffectiveVocalRange) and rendered on the
// detail card as SetSongView.VocalRangeDisplay; a song with no range on
// file, or an effective/reference key that doesn't parse, gets "" (no line
// shown) - same best-effort convention as keyTransitionWarnings' own
// unparseable-key skip.
//
// vocalistPersonIDs/personVocalRanges/vocalRangeCheckNames are Phase 3's
// mismatch-warning inputs (buildSetBuilderData) - see
// vocalRangeWarningForSong, which this calls per row (only when the row
// actually has both a stored song range and a parseable effective key)
// to fill SetSongView.VocalRangeWarningLabel/Message/IsTip.
func renderServiceOrder(ctx context.Context, store *scheduling.Store, organizationID string, arrangementCache map[string]scheduling.SongArrangementInfo, attachmentCache map[string][]scheduling.SongAttachmentInfo, vocalRangesBySong map[string]scheduling.SongVocalRange, noteCategories []pco.ItemNoteCategoryData, itemNotes map[string][]pco.ItemNoteData, rows []mergedRow, titles map[string]string, rolesBySong map[string][]scheduling.SongRole, roleTypeLabels map[string]string, repeated map[string]bool, conflictTitlesBySong map[string][]string, transitionsByRow map[int64]keyTransitionWarning, leaderNames map[string]string, transitionNotesBySongID map[string][]scheduling.TransitionNote, transitionNotePromptsByRow map[int64][]transitionNotePrompt, vocalistPersonIDs []string, personVocalRanges map[string]scheduling.PersonVocalRange, vocalRangeCheckNames map[string]string) []views.ServiceOrderRow {
	// A plain sequential loop, not scheduling.RunConcurrently - unlike
	// before Phase 2 (docs/plans/shipped/pco-call-efficiency-rollout-plan.md), a song
	// row's only per-row lookup, resolveArrangementCacheInfo, no longer
	// makes a live PCO call on a cache miss: it enqueues a background sync
	// request and returns immediately (see its own doc comment), so there's
	// no per-row latency left here to fan out - buildSongNoteViews is
	// likewise a pure map lookup, since item notes are sideloaded once on
	// the GetItems call above rather than fetched per row. Concurrency
	// bought nothing once neither per-row lookup can block on Planning
	// Center any more.
	out := make([]views.ServiceOrderRow, len(rows))

	for i, row := range rows {
		if row.Item != nil {
			label := row.Item.Attributes.Title
			if label == "" {
				label = row.Item.Attributes.ItemType
			}
			out[i] = views.ServiceOrderRow{
				IsSong:            false,
				ItemID:            row.Item.ID,
				ItemTitle:         label,
				ItemType:          row.Item.Attributes.ItemType,
				ItemLength:        formatLengthSeconds(row.Item.Attributes.Length),
				ItemLengthSeconds: row.Item.Attributes.Length,
				ItemDescription:   row.Item.Attributes.Description,
			}
			continue
		}

		ss := row.Song
		roleViews, _ := songRoleViews(rolesBySong, roleTypeLabels, ss.SongID)
		// One lookup covers both ArrangementInfo (the formatted summary
		// line) and the arrangement's Notes, which itemDescriptionFor
		// (scheduling.go) falls back to as a publish-time Description
		// default - see resolveArrangementCacheInfo's own doc comment for
		// why this isn't two separate resolves.
		arrangementInfo := resolveArrangementCacheInfo(ctx, store, organizationID, arrangementCache, ss.SongID)
		key := effectiveKey(*ss, arrangementInfo)
		// The summary line shown right on the row uses the *effective*
		// key, not the arrangement's own cached one - without this, saving
		// a key override in the detail card changed nothing visible
		// anywhere else on the page until the next Publish/Refresh
		// actually pushed it, which read as "did that do anything?"
		// Swapping just Key here (not the whole struct) leaves BPM/meter/
		// length showing the arrangement's real values, since those have
		// no per-occurrence override to reflect.
		displayInfo := arrangementInfo
		displayInfo.Key = key

		songView := views.SetSongView{
			ID:               ss.ID,
			SongID:           ss.SongID,
			Title:            titleOrID(titles, ss.SongID),
			Position:         ss.Position,
			Roles:            roleViews,
			Published:        ss.PCOItemID != "",
			AnchorItemID:     ss.AnchorItemID,
			Moved:            ss.Moved,
			InfoPending:      ss.InfoPending,
			ArrangementInfo:  formatArrangementInfo(displayInfo),
			Structure:        displayInfo.Structure,
			HasChordChart:    displayInfo.HasChordChart,
			Description:      ss.Description,
			Key:              key,
			PCOItemID:        ss.PCOItemID,
			Notes:            buildSongNoteViews(ss.PCOItemID, noteCategories, itemNotes),
			RecentlyRepeated: repeated[ss.SongID],
			ConflictTitles:   conflictTitlesBySong[ss.SongID],
			Attachments:      songAttachmentViews(attachmentCache[ss.SongID]),
			VocalRange:       vocalRangesBySong[ss.SongID],
			LeaderPersonID:   ss.LeaderPersonID,
			LeaderPersonName: titleOrID(leaderNames, ss.LeaderPersonID),
		}
		if transition, ok := transitionsByRow[ss.ID]; ok {
			songView.KeyTransitionLabel = transition.Label
			songView.KeyTransitionMessage = transition.Message
			songView.KeyTransitionSuggestion = transition.Suggestion
		}
		// One songEffectiveVocalRangeAbs call covers VocalRangeDisplay, the
		// tap-to-hear ints it now carries alongside (VocalRangeLowPitchClass
		// etc - see that field's own doc comment, set_types.go), and the
		// mismatch-warning check right below, rather than each re-deriving
		// the same transposed range independently.
		if songLowAbs, songHighAbs, ok := songEffectiveVocalRangeAbs(vocalRangesBySong, ss.SongID, key); ok {
			lowPC, lowOct := scheduling.PitchClassAndOctave(songLowAbs)
			highPC, highOct := scheduling.PitchClassAndOctave(songHighAbs)
			songView.VocalRangeDisplay = fmt.Sprintf("%s-%s", formatVocalNote(lowPC, lowOct), formatVocalNote(highPC, highOct))
			songView.VocalRangeHasPlayableNotes = true
			songView.VocalRangeLowPitchClass = lowPC
			songView.VocalRangeLowOctave = lowOct
			songView.VocalRangeHighPitchClass = highPC
			songView.VocalRangeHighOctave = highOct

			warning := vocalRangeWarningForSong(songLowAbs, songHighAbs, vocalistPersonIDs, personVocalRanges, vocalRangeCheckNames)
			songView.VocalRangeWarningLabel = warning.Label
			songView.VocalRangeWarningMessage = warning.Message
			songView.VocalRangeWarningIsTip = warning.IsTip
		}
		songView.TransitionNotes = buildTransitionNoteViews(transitionNotesBySongID[ss.SongID], ss.SongID, titles)
		for _, p := range transitionNotePromptsByRow[ss.ID] {
			songView.TransitionNotePrompts = append(songView.TransitionNotePrompts, views.TransitionNotePromptView{
				Kind:    p.Kind,
				Label:   p.Label,
				Message: p.Message,
			})
		}

		out[i] = views.ServiceOrderRow{IsSong: true, Song: songView}
	}

	return out
}

// songEffectiveVocalRangeAbs is the absolute-semitone core renderServiceOrder
// (above) and vocalRangeWarningForSong (below) both need - songID's stored
// range (vocalRangesBySong) transposed into effectiveKey (this row's own
// effective key - the local override when set, falling back to the
// arrangement's default). renderServiceOrder formats the result into
// SetSongView.VocalRangeDisplay ("G3-D5") and its play-button pitch-class/
// octave ints in one pass, then reuses the same lowAbs/highAbs for the
// mismatch-warning check right after, so this runs
// scheduling.EffectiveVocalRange exactly once per row rather than each
// caller re-deriving it. ok is false when songID has no stored range on
// file, or when either the stored reference key or effectiveKey fails to
// parse - same best-effort convention as keyTransitionWarnings' own
// unparseable-key skip.
func songEffectiveVocalRangeAbs(vocalRangesBySong map[string]scheduling.SongVocalRange, songID, effectiveKey string) (lowAbs, highAbs int, ok bool) {
	vr, ok := vocalRangesBySong[songID]
	if !ok {
		return 0, 0, false
	}
	return scheduling.EffectiveVocalRange(vr.ReferenceKey, vr.LowestPitchClass, vr.LowestOctave, vr.HighestPitchClass, vr.HighestOctave, effectiveKey)
}

// vocalistsWithRangesOnFile intersects personIDs (a plan's roster
// assignments, possibly with duplicates) with ranges (every
// PersonVocalRange on file for the org) - the exact set of people
// vocalRangeCheckNames (buildSetBuilderData) needs a display name for, so
// personNameMapForIDs is never called for a roster member who couldn't
// possibly end up in a mismatch message anyway (no range on file at all).
func vocalistsWithRangesOnFile(personIDs []string, ranges map[string]scheduling.PersonVocalRange) []string {
	seen := make(map[string]bool, len(personIDs))
	ids := make([]string, 0, len(personIDs))
	for _, id := range personIDs {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		if _, ok := ranges[id]; ok {
			ids = append(ids, id)
		}
	}
	return ids
}

// vocalRangeWarning is one song row's own Phase 3 verdict (docs/plans/
// in-progress/vocal-range-plan.md) - see vocalRangeWarningForSong. Label ==
// "" means no vocalist assigned to this plan's roster mismatches this
// song's range at all.
type vocalRangeWarning struct {
	Label   string
	Message string
	IsTip   bool
}

// vocalRangeWarningForSong checks songLowAbs/songHighAbs (this song's own
// effective, transposed range - see songEffectiveVocalRangeAbs) against
// every one of vocalistPersonIDs (this plan's roster assignments,
// buildSetBuilderData - may contain duplicates and non-vocalists, both
// handled below) who also has a PersonVocalRange on file in ranges -
// that's the whole "who counts as a vocalist to check" rule (a range on
// file is the proxy for "this is a vocalist," not a team/position-name
// heuristic this app would otherwise have to guess at), per the plan's own
// Phase 3 section.
//
// Before flagging a hard mismatch for someone, this also checks the
// song's range shifted a full octave up and a full octave down (see
// vocalRangeFitsShifted) - a register difference between voice types
// (e.g. a tenor singing an alto line down an octave) is the single most
// common real fix for an apparent mismatch, so a shift that brings the
// song's range fully inside that vocalist's own downgrades their line
// from a mismatch to a same-Disclosure tip, worded as a suggestion rather
// than a conflict (see the plan's "Octave-shift tip" section).
//
// A hard mismatch for even one vocalist wins over every other vocalist's
// mere tip: the returned warning renders as a real warning (IsTip false,
// conflict-badge styling in songBadgeSpans) whenever hardLines is
// non-empty, with every affected vocalist's own line (mismatch or tip)
// folded into one combined Message so nothing found gets silently dropped
// from the explanation - only when every affected vocalist's own issue is
// octave-shift-fixable does the whole badge downgrade to a tip.
//
// Zero value (Label == "") means nobody assigned mismatches at all - by
// far the common case: no roster yet, nobody assigned has a range on
// file, or every fitting range already covers this song as written.
func vocalRangeWarningForSong(songLowAbs, songHighAbs int, vocalistPersonIDs []string, ranges map[string]scheduling.PersonVocalRange, personNames map[string]string) vocalRangeWarning {
	var hardLines, tipLines []string
	seen := make(map[string]bool, len(vocalistPersonIDs))

	for _, personID := range vocalistPersonIDs {
		if personID == "" || seen[personID] {
			continue
		}
		seen[personID] = true

		pr, ok := ranges[personID]
		if !ok {
			continue // not a vocalist to check - no range on file
		}
		personLow := scheduling.AbsoluteSemitone(pr.LowestPitchClass, pr.LowestOctave)
		personHigh := scheduling.AbsoluteSemitone(pr.HighestPitchClass, pr.HighestOctave)
		if songLowAbs >= personLow && songHighAbs <= personHigh {
			continue // fits as written
		}

		name := titleOrID(personNames, personID)
		personRangeText := fmt.Sprintf("%s-%s", formatVocalNote(pr.LowestPitchClass, pr.LowestOctave), formatVocalNote(pr.HighestPitchClass, pr.HighestOctave))

		if fits, direction := vocalRangeFitsShifted(songLowAbs, songHighAbs, personLow, personHigh); fits {
			tipLines = append(tipLines, fmt.Sprintf("%s's range is %s - fits if sung an octave %s than written.", name, personRangeText, direction))
			continue
		}

		reach := strings.Join(vocalRangeViolationNotes(songLowAbs, songHighAbs, personLow, personHigh), " and ")
		hardLines = append(hardLines, fmt.Sprintf("%s's range is %s; this song's current key needs %s to reach %s.", name, personRangeText, name, reach))
	}

	switch {
	case len(hardLines) > 0:
		return vocalRangeWarning{
			Label:   "range mismatch",
			Message: strings.Join(append(hardLines, tipLines...), " "),
		}
	case len(tipLines) > 0:
		return vocalRangeWarning{
			Label:   "range tip",
			Message: strings.Join(tipLines, " "),
			IsTip:   true,
		}
	default:
		return vocalRangeWarning{}
	}
}

// vocalRangeFitsShifted checks whether shifting the song's own range a
// full octave up or down would bring it fully inside [personLow,
// personHigh] - see vocalRangeWarningForSong's own doc comment on why an
// octave-shift check happens before a mismatch is treated as a hard one.
// Checked down first (by far the more common real-world direction -
// "sing it an octave lower" - though whichever direction actually fits is
// what gets reported; if both fit, down wins).
func vocalRangeFitsShifted(songLowAbs, songHighAbs, personLow, personHigh int) (fits bool, direction string) {
	if songLowAbs-12 >= personLow && songHighAbs-12 <= personHigh {
		return true, "lower"
	}
	if songLowAbs+12 >= personLow && songHighAbs+12 <= personHigh {
		return true, "higher"
	}
	return false, ""
}

// vocalRangeViolationNotes names the specific note(s) that put the song's
// range outside [personLow, personHigh] - "down to <note>" when the
// song's low note sits below the vocalist's own low note, "up to <note>"
// when the song's high note sits above the vocalist's own high note,
// either or both (a vocalist's range can be narrower than the song's on
// both ends at once).
func vocalRangeViolationNotes(songLowAbs, songHighAbs, personLow, personHigh int) []string {
	var notes []string
	if songLowAbs < personLow {
		notes = append(notes, fmt.Sprintf("down to %s", formatVocalNote(scheduling.PitchClassAndOctave(songLowAbs))))
	}
	if songHighAbs > personHigh {
		notes = append(notes, fmt.Sprintf("up to %s", formatVocalNote(scheduling.PitchClassAndOctave(songHighAbs))))
	}
	return notes
}

// songRolesBySong buckets every one of an org's song roles by song id - the
// role equivalent of songTagsBySong (scheduling.go).
func songRolesBySong(roles []scheduling.SongRole) map[string][]scheduling.SongRole {
	bySong := make(map[string][]scheduling.SongRole, len(roles))
	for _, r := range roles {
		bySong[r.SongID] = append(bySong[r.SongID], r)
	}
	return bySong
}

// songRoleViews resolves songID's roles (via rolesBySong, see
// songRolesBySong) to display view models (see views.SongRoleView) plus
// their space-joined codes (RolesAttr - matched server-side against a
// role-filter query param by candidateMatchesRole below, for the search
// endpoint). Shared by every place a song's roles need to go on screen: set
// songs, order-of-service rows, and not-yet-added candidates.
func songRoleViews(rolesBySong map[string][]scheduling.SongRole, roleTypeLabels map[string]string, songID string) ([]views.SongRoleView, string) {
	roles := rolesBySong[songID]
	if len(roles) == 0 {
		return nil, ""
	}

	roleViews := make([]views.SongRoleView, 0, len(roles))
	codes := make([]string, 0, len(roles))
	for _, r := range roles {
		roleViews = append(roleViews, views.SongRoleView{Code: r.Role, Label: titleOrID(roleTypeLabels, r.Role)})
		codes = append(codes, r.Role)
	}
	return roleViews, strings.Join(codes, " ")
}

// songTagViews resolves songID's tags (via tagsBySong, see songTagsBySong)
// to display view models (see views.SongTagView) plus their space-joined,
// lowercased values (TagsAttr - matched server-side against a typed search
// term by candidateMatchesSearch below) - the tag equivalent of
// songRoleViews above. Shared by every not-yet-added-song list that shows
// tag badges: the candidate search list and the suggestion lists (see
// SuggestionView, set_types.go) alike.
func songTagViews(tagsBySong map[string][]scheduling.SongTag, tagCategoryLabels map[string]string, songID string) ([]views.SongTagView, string) {
	tags := tagsBySong[songID]
	if len(tags) == 0 {
		return nil, ""
	}

	tagViews := make([]views.SongTagView, 0, len(tags))
	values := make([]string, 0, len(tags))
	for _, t := range tags {
		tagViews = append(tagViews, views.SongTagView{Category: titleOrID(tagCategoryLabels, t.Category), Value: t.Value})
		values = append(values, strings.ToLower(t.Value))
	}
	return tagViews, strings.Join(values, " ")
}

// titleOrID looks up id in a string->string map, falling back to id itself
// when there's no entry - originally just for song titles, but the same
// "resolved label, or the raw code if it doesn't resolve" shape fits any
// code->label map, e.g. tag category labels above.
func titleOrID(titles map[string]string, id string) string {
	if title, ok := titles[id]; ok {
		return title
	}
	return id
}

// missingSongIDs returns the subset of songIDs (deduplicated, "" skipped)
// that aren't already a key in titles - pure and testable independent of
// any PCO call, so resolveMissingTitles' actual live-fetch logic can stay
// thin.
func missingSongIDs(titles map[string]string, songIDs []string) []string {
	seen := make(map[string]bool, len(songIDs))
	var missing []string
	for _, id := range songIDs {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		if _, ok := titles[id]; !ok {
			missing = append(missing, id)
		}
	}
	return missing
}

// resolveMissingTitles backfills titles (in place) for any songID in
// songIDs that's missing from it - e.g. because fetchActiveSongs'
// archived-song filter excluded it after it was scheduled while still
// active - with one pco.GetSong call per still-missing id. Typically zero
// calls in practice: bounded by however many stale references exist in
// this one set/roster, not by the size of the song library. Shared by
// sets_builder.go's SetSongs/pendingDeletions and sets_publish.go's
// PublishSet/RefreshSongInfo - all four resolve titles for songs already
// referenced by a local set-song row, as opposed to AllSongs/the
// add-a-song candidate pool, which is deliberately active-only with no
// such backfill.
//
// Best-effort, same tolerance as every other title lookup in this file: a
// failed individual GetSong just leaves that one id unresolved, falling
// back to titleOrID's raw-ID display - not a new failure mode, the
// pre-existing "song not found in the title map" degrade path, just one
// this backfill narrows rather than widens.
func resolveMissingTitles(ctx context.Context, titles map[string]string, songIDs []string) {
	for _, id := range missingSongIDs(titles, songIDs) {
		if song, err := pco.GetSong(ctx, id); err == nil {
			titles[id] = song.Data.Attributes.Title
		}
	}
}

func appendErr(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return a + "; " + b
}
