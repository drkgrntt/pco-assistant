package controllers

// Assembling views.SetBuilderData: merging local set songs into the plan's
// real order of service, plus the pure helpers this leans on (also covered
// by sets_builder_test.go).

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

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
func (sc *SchedulingController) buildSetBuilderData(ctx context.Context, organizationID, serviceTypeID, planID string) views.SetBuilderData {
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

	// AllSongs/TagCategories/RelationshipTypes/RoleTypes back the "Quick
	// add" panel's <select> pickers (see views.SetBuilderData) - best-effort
	// like the lookups above, since a failure here only costs that one
	// panel's dropdowns, not the page.
	data.AllSongs = songs
	data.TagCategories = tagCategories
	data.RelationshipTypes, _ = sc.store.ListSongRelationshipTypes(ctx)
	data.RoleTypes = roleTypes

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
	// than fetched right where RepeatWarnings gets built) so
	// history.AverageLength is already in hand by the time the
	// ServiceOrder block below needs it.
	history := fetchRecentPlanHistory(ctx, serviceTypeID)

	// ServiceOrder: the plan's full order, with this set's own songs
	// interleaved at their anchored position (see mergeServiceOrder) -
	// replaces the old song-only "current set" list, since seeing (and
	// placing songs against) the whole order of service, template headers
	// included, is the point.
	if pcoItems, err := pco.GetItems(ctx, serviceTypeID, planID, nil); err != nil {
		data.ErrMsg = appendErr(data.ErrMsg, "loading plan items from Planning Center: "+err.Error())
	} else {
		sort.Slice(pcoItems.Data, func(i, j int) bool {
			return pcoItems.Data[i].Attributes.Sequence < pcoItems.Data[j].Attributes.Sequence
		})
		merged := mergeServiceOrder(pcoItems.Data, setSongs)
		// Fetched once per render, not per song - see renderServiceOrder's
		// own doc comment on noteCategories.
		noteCategories, _ := pco.GetItemNoteCategories(ctx, serviceTypeID, nil)
		data.ServiceOrder = renderServiceOrder(ctx, sc.store, organizationID, serviceTypeID, planID, arrangementCache, noteCategories.Data, merged, titles, rolesBySong, roleTypeLabels)

		// PlanWarnings: whole-plan concerns, as opposed to the per-song/
		// per-pairing warnings below - see serviceLengthWarning and
		// keyTransitionWarnings for what each actually checks. Both work
		// off merged/arrangementCache, already in hand here, and
		// history.AverageLength, already fetched above - zero new PCO
		// calls for either.
		if warning := serviceLengthWarning(serviceOrderLengthSeconds(merged, arrangementCache), history.AverageLength); warning != "" {
			data.PlanWarnings = append(data.PlanWarnings, warning)
		}
		data.PlanWarnings = append(data.PlanWarnings, keyTransitionWarnings(merged, arrangementCache, titles)...)

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

	// candidateConflicts/existing and repeated (below) are fetched up front,
	// ahead of both the Suggestions loop and the SongsToReinforce pass
	// further down, since "Recommended next"/"Could use one more sing" (see
	// SuggestionView, set_types.go) show the same conflict/repeat/
	// arrangement/role/tag metadata the "Browse all songs" search list does
	// (see candidateLookups.candidateSongView), not just title + reason.
	candidateConflicts, existing, err := sc.store.AvoidPairings(ctx, organizationID, set.ID)
	if err != nil {
		data.ErrMsg = appendErr(data.ErrMsg, err.Error())
	}
	for _, ex := range existing {
		data.ExistingConflicts = append(data.ExistingConflicts, fmt.Sprintf(
			"%s conflicts with %s - both are already in this set.",
			titleOrID(titles, ex.SongID), titleOrID(titles, ex.ConflictsWith),
		))
	}

	// repeated: songs sung in both of the last two past plans for this
	// service type, straight from Planning Center's own history rather than
	// this tool's local sets - see fetchRecentPlanHistory. Surfaced both
	// against songs already in this set (a banner, alongside the same-set
	// avoid-pairing conflicts above) and against not-yet-added candidates
	// (a badge, so it's visible before adding rather than only after).
	repeated := history.RepeatedSongs
	for _, ss := range data.SetSongs {
		if repeated[ss.SongID] {
			data.RepeatWarnings = append(data.RepeatWarnings, fmt.Sprintf(
				"%s was sung in each of the last two plans - consider whether it should repeat again so soon.",
				titleOrID(titles, ss.SongID),
			))
		}
	}

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
		ArrangementInfo:  arrangementInfo,
		Roles:            roleViews,
		RolesAttr:        rolesAttr,
		Tags:             tagViews,
		TagsAttr:         tagsAttr,
	}
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
// neither set, this deliberately returns the empty-prompt state rather
// than the first candidateSearchLimit songs in the library - not fetching
// or computing anything until someone actually searches is the entire
// point of this being server-driven instead of eagerly rendering
// everything (see CandidateSearchResults' own doc comment for the four
// states it can render). Matches are capped at candidateSearchLimit; the
// results view shows a "showing top N of M" hint whenever that cap is
// actually hit.
func (sc *SchedulingController) BuildCandidatesSearch(c *fiber.Ctx) error {
	organizationID := middleware.CurrentOrganizationID(c)
	ctx := middleware.RequestContext(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")
	query := c.Query("q")
	role := c.Query("role")

	candidates, totalMatches, capped := sc.searchCandidates(ctx, organizationID, serviceTypeID, planID, query, role)
	return render(c, views.CandidateSearchResults(serviceTypeID, planID, candidates, query, role, totalMatches, capped))
}

// searchCandidates is BuildCandidatesSearch's actual work, split out so the
// pure matching/capping logic below (candidateMatchesSearch/
// candidateMatchesRole/capCandidates) can be unit tested without a live
// PCO/DB round trip. Fetches its own copy of the same lookups
// buildSetBuilderData gathers for its SongsToReinforce pass (roles, tags,
// avoid-pairing conflicts, arrangement cache, repeat history, reinforcement
// tracking) - cheaper than it sounds despite the duplication: none of
// buildSetBuilderData's plan-item/order-of-service fetches (GetPlan,
// GetItems, note categories) are needed here, fetchActiveSongs itself is
// the same 60s-cached call every other Build page hit already reuses (see
// docs/pco-api-risks.md), and - the actual point of this endpoint existing -
// only the songs that actually match query/role ever get the full
// candidateSongView treatment, capped at candidateSearchLimit, rather than
// the whole library. Best-effort throughout, same convention as
// buildSetBuilderData: a failed lookup just degrades whatever it backs
// (e.g. no conflict badges) instead of failing the search outright.
func (sc *SchedulingController) searchCandidates(ctx context.Context, organizationID, serviceTypeID, planID, query, role string) (results []views.CandidateSongView, totalMatches int, capped bool) {
	set, err := sc.store.GetOrCreateSet(ctx, organizationID, serviceTypeID, planID)
	if err != nil {
		return nil, 0, false
	}
	setSongs, err := sc.store.ListSetSongs(ctx, set.ID)
	if err != nil {
		return nil, 0, false
	}
	inSet := make(map[string]bool, len(setSongs))
	for _, ss := range setSongs {
		inSet[ss.SongID] = true
	}

	songs, _ := fetchActiveSongs(ctx, organizationID)
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

	candidateConflicts, _, _ := sc.store.AvoidPairings(ctx, organizationID, set.ID)
	history := fetchRecentPlanHistory(ctx, serviceTypeID)

	lookups := candidateLookups{
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
		_, rolesAttr := songRoleViews(rolesBySong, roleTypeLabels, song.ID)
		if !candidateMatchesRole(rolesAttr, role) {
			continue
		}
		_, tagsAttr := songTagViews(tagsBySong, tagCategoryLabels, song.ID)
		if !candidateMatchesSearch(song.Attributes.Title, tagsAttr, query) {
			continue
		}
		matches = append(matches, lookups.candidateSongView(song))
	}

	totalMatches = len(matches)
	results, capped = capCandidates(matches, candidateSearchLimit)
	return results, totalMatches, capped
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

// keyPitchClasses maps a note letter to its pitch class (C=0 .. B=11) -
// keyTransitionWarnings' only music-theory lookup table.
var keyPitchClasses = map[string]int{
	"C": 0, "D": 2, "E": 4, "F": 5, "G": 7, "A": 9, "B": 11,
}

// parseKeyPitchClass parses a key string ("Bb", "F#m", "G") into a 0-11
// pitch class, ignoring major/minor quality entirely - a trailing "m"
// (minor - see resolveKeyID's own comment on this convention) is accepted
// but doesn't change the result, since this warning is about root
// movement, not mode. Returns ok=false for anything that isn't a bare note
// letter A-G optionally followed by a single "#"/"♯" or "b"/"♭" - good
// enough for what this app and Planning Center's own key picker actually
// produce, not a general music-notation parser.
func parseKeyPitchClass(key string) (int, bool) {
	key = strings.TrimSpace(key)
	key = strings.TrimSuffix(key, "m")
	if key == "" {
		return 0, false
	}

	root, ok := keyPitchClasses[strings.ToUpper(key[:1])]
	if !ok {
		return 0, false
	}
	switch key[1:] {
	case "":
	case "#", "♯":
		root++
	case "b", "♭":
		root--
	default:
		return 0, false
	}
	return ((root % 12) + 12) % 12, true
}

// semitoneDistance is the minimal circular distance (0-6) between two pitch
// classes - 1 either direction (e.g. E to F, or B to C) is a half step, the
// interval keyTransitionWarnings actually flags.
func semitoneDistance(a, b int) int {
	d := a - b
	if d < 0 {
		d = -d
	}
	if d > 6 {
		d = 12 - d
	}
	return d
}

// keyTransitionWarnings flags two songs that are truly back-to-back in the
// order - nothing else between them in rows, see mergeServiceOrder - whose
// keys are a half-step apart: the single interval most likely to sound like
// a mistake rather than an intentional change, and the hardest for a
// guitarist to prepare for (a full re-fret, not a capo move). A song with
// no key set, or a key string that doesn't parse (see
// parseKeyPitchClass), is silently skipped - same best-effort convention as
// fetchRecentPlanHistory above.
func keyTransitionWarnings(rows []mergedRow, arrangementCache map[string]scheduling.SongArrangementInfo, titles map[string]string) []string {
	var warnings []string
	for i := 1; i < len(rows); i++ {
		prev, cur := rows[i-1], rows[i]
		if prev.Song == nil || cur.Song == nil {
			continue
		}

		prevKey := effectiveKey(*prev.Song, arrangementCache[prev.Song.SongID])
		curKey := effectiveKey(*cur.Song, arrangementCache[cur.Song.SongID])
		prevPitch, ok := parseKeyPitchClass(prevKey)
		if !ok {
			continue
		}
		curPitch, ok := parseKeyPitchClass(curKey)
		if !ok {
			continue
		}
		change := semitoneDistance(prevPitch, curPitch)
		// minor second or tri tone
		if change != 1 && change != 6 {
			continue
		}

		if change == 1 {
			warnings = append(warnings, fmt.Sprintf(
				"%s (%s) into %s (%s) is a half-step key change with no transition - consider a pad/bridge, or adjusting one song's key.",
				titleOrID(titles, prev.Song.SongID), prevKey,
				titleOrID(titles, cur.Song.SongID), curKey,
			))
		} else if change == 6 {
			warnings = append(warnings, fmt.Sprintf(
				"%s (%s) into %s (%s) is a tri-tone key change with no transition - consider a pad/bridge, or adjusting one song's key.",
				titleOrID(titles, prev.Song.SongID), prevKey,
				titleOrID(titles, cur.Song.SongID), curKey,
			))
		}
	}
	return warnings
}

// buildSongNoteViews resolves one views.SongNoteView per available note
// category for a published song's plan item (itemID) - "" means
// unpublished, and there's no item to attach a note to yet, so this
// returns nil without a wasted PCO call. Best-effort like everything else
// on this page: a failed GetItemNotes just means every category shows
// empty (create-on-save) rather than a broken page.
func buildSongNoteViews(ctx context.Context, serviceTypeID, planID, itemID string, noteCategories []pco.ItemNoteCategoryData) []views.SongNoteView {
	if itemID == "" || len(noteCategories) == 0 {
		return nil
	}

	existing, _ := pco.GetItemNotes(ctx, serviceTypeID, planID, itemID, nil)
	byCategory := make(map[string]pco.ItemNoteData, len(existing.Data))
	for _, n := range existing.Data {
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
// categories (Audio/Visual, Band, Vocals, etc.), fetched once by the
// caller and reused for every published song's SongNoteView (see
// buildSongNoteViews) - a service type has few of these, so one shared
// fetch instead of a category lookup per song.
func renderServiceOrder(ctx context.Context, store *scheduling.Store, organizationID, serviceTypeID, planID string, arrangementCache map[string]scheduling.SongArrangementInfo, noteCategories []pco.ItemNoteCategoryData, rows []mergedRow, titles map[string]string, rolesBySong map[string][]scheduling.SongRole, roleTypeLabels map[string]string) []views.ServiceOrderRow {
	// Each row is independent of every other - a foreign item is pure local
	// data, and a song row's PCO calls (resolveArrangementCacheInfo,
	// buildSongNoteViews) only read arrangementCache/noteCategories, never
	// write them (resolveArrangementCacheInfo's cache-miss path upserts to
	// Postgres, not this in-memory map - see its own doc comment), so
	// there's nothing here for concurrent rows to race on. out is pre-sized
	// and each goroutine only ever writes its own index, so no lock is
	// needed for that either. See docs/pco-api-risks.md's concurrency
	// mitigation - this replaces a sequential per-row loop that paid every
	// song's GetItemNotes/GetArrangements/GetKeys latency one at a time.
	out := make([]views.ServiceOrderRow, len(rows))

	indexes := make([]int, len(rows))
	for i := range rows {
		indexes[i] = i
	}

	scheduling.RunConcurrently(indexes, func(i int) {
		row := rows[i]
		if row.Item != nil {
			label := row.Item.Attributes.Title
			if label == "" {
				label = row.Item.Attributes.ItemType
			}
			out[i] = views.ServiceOrderRow{
				IsSong:    false,
				ItemID:    row.Item.ID,
				ItemTitle: label,
				ItemType:  row.Item.Attributes.ItemType,
			}
			return
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
		out[i] = views.ServiceOrderRow{
			IsSong: true,
			Song: views.SetSongView{
				ID:              ss.ID,
				SongID:          ss.SongID,
				Title:           titleOrID(titles, ss.SongID),
				Position:        ss.Position,
				Roles:           roleViews,
				Published:       ss.PCOItemID != "",
				AnchorItemID:    ss.AnchorItemID,
				Moved:           ss.Moved,
				InfoPending:     ss.InfoPending,
				ArrangementInfo: formatArrangementInfo(displayInfo),
				Description:     ss.Description,
				Key:             key,
				PCOItemID:       ss.PCOItemID,
				Notes:           buildSongNoteViews(ctx, serviceTypeID, planID, ss.PCOItemID, noteCategories),
			},
		}
	})

	return out
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
