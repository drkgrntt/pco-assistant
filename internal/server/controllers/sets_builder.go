package controllers

// Assembling views.SetBuilderData: merging local set songs into the plan's
// real order of service, plus the pure helpers this leans on (also covered
// by sets_builder_test.go).

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"pco-assistant/internal/scheduling"
	views "pco-assistant/internal/views/scheduling"

	pco "github.com/drkgrntt/go-pco"
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

	songs, songsErr := fetchAllSongs(ctx)
	if songsErr != nil {
		data.ErrMsg = appendErr(data.ErrMsg, "loading songs from Planning Center: "+songsErr.Error())
	}
	titles := songTitleMap(songs)

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
	}

	suggestions, err := sc.store.Recommendations(ctx, organizationID, set.ID)
	if err != nil {
		data.ErrMsg = appendErr(data.ErrMsg, err.Error())
	}
	for _, sug := range suggestions {
		data.Suggestions = append(data.Suggestions, views.SuggestionView{
			SongID:            sug.SongID,
			Title:             titleOrID(titles, sug.SongID),
			RelationshipLabel: strings.Join(sug.RelationshipTypes, ", "),
		})
	}

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
	// this tool's local sets - see recentlyRepeatedSongs. Surfaced both
	// against songs already in this set (a banner, alongside the same-set
	// avoid-pairing conflicts above) and against not-yet-added candidates
	// (a badge, so it's visible before adding rather than only after).
	repeated := recentlyRepeatedSongs(ctx, serviceTypeID)
	for _, ss := range data.SetSongs {
		if repeated[ss.SongID] {
			data.RepeatWarnings = append(data.RepeatWarnings, fmt.Sprintf(
				"%s was sung in each of the last two plans - consider whether it should repeat again so soon.",
				titleOrID(titles, ss.SongID),
			))
		}
	}

	for _, song := range songs {
		if inSet[song.ID] {
			continue
		}

		roleViews, rolesAttr := songRoleViews(rolesBySong, roleTypeLabels, song.ID)

		var conflictTitles []string
		for _, conflictSongID := range candidateConflicts[song.ID] {
			conflictTitles = append(conflictTitles, titleOrID(titles, conflictSongID))
		}

		var tagViews []views.SongTagView
		var tagValues []string
		for _, tag := range tagsBySong[song.ID] {
			tagViews = append(tagViews, views.SongTagView{
				Category: titleOrID(tagCategoryLabels, tag.Category),
				Value:    tag.Value,
			})
			tagValues = append(tagValues, strings.ToLower(tag.Value))
		}

		newSongLabel := reinforcementLabel(trackedNew, appearanceCounts, song.ID)

		// Cache-only, deliberately no live-fetch fallback here (unlike
		// resolveArrangementInfo for ServiceOrder above) - Planning Center
		// has no bulk "arrangements for every song" call, so doing this for
		// the whole candidate library would cost one request per song on
		// every render. An uncached candidate just shows nothing until it's
		// either added to a set once (which does the live fetch/backfill)
		// or covered by a Song Info sync.
		var arrangementInfo string
		if info, ok := arrangementCache[song.ID]; ok {
			arrangementInfo = formatArrangementInfo(info)
		}

		data.Candidates = append(data.Candidates, views.CandidateSongView{
			SongID:           song.ID,
			Title:            song.Attributes.Title,
			ConflictTitles:   conflictTitles,
			RecentlyRepeated: repeated[song.ID],
			NewSongLabel:     newSongLabel,
			ArrangementInfo:  arrangementInfo,
			Roles:            roleViews,
			RolesAttr:        rolesAttr,
			Tags:             tagViews,
			TagsAttr:         strings.Join(tagValues, " "),
		})

		// SongsToReinforce is its own list, not merged into data.Suggestions:
		// Suggestions is anchored to the last song in the set and would stay
		// empty on an empty set, but a still-new song deserves a nudge
		// regardless of what's already in the set - see set_types.go.
		switch newSongLabel {
		case "Sung once":
			data.SongsToReinforce = append(data.SongsToReinforce, views.SuggestionView{
				SongID:            song.ID,
				Title:             song.Attributes.Title,
				RelationshipLabel: "Sung once - one more to make it stick",
			})
		case "Sung twice":
			data.SongsToReinforce = append(data.SongsToReinforce, views.SuggestionView{
				SongID:            song.ID,
				Title:             song.Attributes.Title,
				RelationshipLabel: "Sung twice - almost there",
			})
		}
	}

	return data
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
// recentlyRepeatedSongs compares - "the two sets before it", per the
// feature's own description.
const recentTwoPlanLookback = 2

// recentlyRepeatedSongs returns the song IDs sung in *both* of the two most
// recent past plans for a service type, straight from Planning Center's own
// item data - not this tool's local sets, since the point is to flag a song
// repeating in what actually got sung regardless of whether it was built
// here. Fewer than two past plans, or any lookup failure along the way,
// yields an empty set rather than an error: this is an advisory nicety for
// the builder, not something that should block it. The actual overlap
// computation lives in songsInBoth so it can be unit tested without hitting
// Planning Center.
func recentlyRepeatedSongs(ctx context.Context, serviceTypeID string) map[string]bool {
	plans, err := pco.GetPlans(ctx, serviceTypeID, &pco.PlansParams{
		Filter:  "past",
		OrderBy: "-sort_date",
		PerPage: recentTwoPlanLookback,
	})
	if err != nil || len(plans.Data) < recentTwoPlanLookback {
		return nil
	}

	items, err := pco.GetItems(ctx, serviceTypeID, plans.Data[0].ID, nil)
	if err != nil {
		return nil
	}
	prevItems, err := pco.GetItems(ctx, serviceTypeID, plans.Data[1].ID, nil)
	if err != nil {
		return nil
	}

	return songsInBoth(items.Data, prevItems.Data)
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

// mergeServiceOrder interleaves a plan's existing items (already sorted by
// sequence) with a set's songs, each song placed immediately after its
// anchor (SetSong.AnchorItemID), grouped there in local position order;
// songs anchored to "" (top of service) come first, before any existing
// item. An item that's one of the set's own tracked songs is skipped here
// (it renders via its song row instead, not twice). A song anchored to an
// item id that isn't in items - e.g. that item was deleted in PCO since -
// is appended at the very end rather than silently dropped.
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
		if trackedItemIDs[item.ID] {
			continue
		}
		item := item
		rows = append(rows, mergedRow{Item: &item})
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
	out := make([]views.ServiceOrderRow, 0, len(rows))

	for _, row := range rows {
		if row.Item != nil {
			label := row.Item.Attributes.Title
			if label == "" {
				label = row.Item.Attributes.ItemType
			}
			out = append(out, views.ServiceOrderRow{
				IsSong:    false,
				ItemID:    row.Item.ID,
				ItemTitle: label,
				ItemType:  row.Item.Attributes.ItemType,
			})
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
		out = append(out, views.ServiceOrderRow{
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
		})
	}

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
// their space-joined codes for the candidate list's client-side
// role-filter/search (RolesAttr - see filterCandidates in scheduling.js).
// Shared by every place a song's roles need to go on screen: set songs,
// order-of-service rows, and not-yet-added candidates.
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

func appendErr(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return a + "; " + b
}
