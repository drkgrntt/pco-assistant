package controllers

// Publish/Refresh/Revert: syncing the draft set to and from real Planning
// Center plan items.

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"pco-assistant/internal/scheduling"
	"pco-assistant/internal/server/middleware"
	views "pco-assistant/internal/views/scheduling"

	pco "github.com/drkgrntt/go-pco"
	"github.com/gofiber/fiber/v2"
)

// syncLeaderNote writes ss's local song leader (SetSong.LeaderPersonID)
// into the org's configured vocal notes category on its Planning Center
// plan item, if one is configured for this service type - see
// docs/plans/shipped/song-leader-plan.md's "Publish-time sync" section.
// currentNoteID/currentContent are that category's existing note on this
// item right now, if any (""/"" meaning no note there yet) - always
// resolved by the caller from whatever it already sideloaded via
// itemNotesFromIncluded (an Include: []string{"item_notes"} GetItems
// call already being made for another reason), never a fresh per-song
// GetItemNotes call - see PublishSet/RefreshSongInfo's own call sites.
//
// No-op when vocalCategoryID == "" (the feature isn't configured for this
// service type yet - see scheduling.Store.VocalNoteCategoryID) or itemID
// == "" (an unpublished song has no plan item to attach a note to). Also
// a no-op when the composed content wouldn't actually change anything, to
// avoid a pointless PATCH. Uses the exact same pco.CreateItemNote/
// UpdateItemNote/DeleteItemNote calls UpdateSongNote (sets_detail.go)
// already makes for the same category, just driven by the local leader
// field instead of a live form submit.
func syncLeaderNote(ctx context.Context, serviceTypeID, planID, itemID, vocalCategoryID, currentNoteID, currentContent, leaderName string) error {
	if vocalCategoryID == "" || itemID == "" {
		return nil
	}

	newContent := composeLeaderNoteContent(currentContent, leaderName)
	if newContent == currentContent {
		return nil
	}

	switch {
	case newContent == "" && currentNoteID != "":
		return pco.DeleteItemNote(ctx, serviceTypeID, planID, itemID, currentNoteID)
	case newContent == "":
		return nil
	case currentNoteID != "":
		_, err := pco.UpdateItemNote(ctx, serviceTypeID, planID, itemID, currentNoteID, &pco.UpdateItemNoteParams{Content: newContent})
		return err
	default:
		_, err := pco.CreateItemNote(ctx, serviceTypeID, planID, itemID, &pco.CreateItemNoteParams{Content: newContent, ItemNoteCategoryID: vocalCategoryID})
		return err
	}
}

// distinctLeaderPersonIDs collects every distinct, non-empty
// LeaderPersonID across setSongs - feeds personNameMapForIDs' batched
// where[id]=... lookup (PublishSet/RefreshSongInfo) so a set with several
// songs sharing the same leader still costs one name lookup, not one per
// song, and a set with no leaders set at all costs zero (an empty slice
// short-circuits personLookupMapForIDs before it makes any call).
func distinctLeaderPersonIDs(setSongs []scheduling.SetSong) []string {
	seen := make(map[string]bool, len(setSongs))
	ids := make([]string, 0, len(setSongs))
	for _, ss := range setSongs {
		if ss.LeaderPersonID == "" || seen[ss.LeaderPersonID] {
			continue
		}
		seen[ss.LeaderPersonID] = true
		ids = append(ids, ss.LeaderPersonID)
	}
	return ids
}

// planStatus classifies a plan for the picker list:
//   - "planned": a local draft exists, has songs, and none are unpublished
//     or queued for deletion - fully synced with Planning Center.
//   - "pending": a local draft exists with unpublished and/or
//     pending-deletion songs - built here, not yet pushed.
//   - "todo": no local draft (or an empty one), but Planning Center already
//     has song items in this plan - probably a template worth building out.
//   - "" : neither - nothing to flag either way.
func (sc *SchedulingController) planStatus(ctx context.Context, organizationID, serviceTypeID, planID string) string {
	if set, found, err := sc.store.FindSet(ctx, organizationID, serviceTypeID, planID); err == nil && found {
		setSongs, songsErr := sc.store.ListSetSongs(ctx, set.ID)
		pending, pendingErr := sc.store.ListPendingDeletions(ctx, set.ID)
		if songsErr == nil && pendingErr == nil && len(setSongs) > 0 {
			for _, ss := range setSongs {
				if ss.PCOItemID == "" {
					return "pending"
				}
			}
			if len(pending) > 0 {
				return "pending"
			}
			return "planned"
		}
	}

	items, err := pco.GetItems(ctx, serviceTypeID, planID, nil)
	if err != nil {
		return ""
	}
	for _, item := range items.Data {
		if item.Attributes.ItemType == pco.ItemTypeSong {
			return "todo"
		}
	}
	return ""
}

// PublishSet fully syncs the draft set to the real Planning Center plan:
//   - songs removed/replaced since the last publish (tombstoned rows, see
//     scheduling.Store.RemoveSongFromSet/ReplaceSongInSet) have their PCO
//     item deleted, then the local tombstone is cleared;
//   - songs never published are created as song-linked items;
//   - the whole plan is then reordered in one call (via pco.ReorderItems -
//     PCO's "item_reorder" plan action; a per-item sequence PATCH is
//     rejected outright) so every tracked song ends up in the set's current
//     local order, appended after whatever untracked ("foreign") items the
//     plan already had. Title is only ever set at creation, never touched
//     by the reorder, so a title someone edited directly in Planning Center
//     isn't clobbered by a reorder.
//
// It never touches a PCO item this tool didn't itself create (tracked via
// pco_item_id) - foreign items keep their existing relative order in the
// resort, and are surfaced read-only as PCOOnlySongs in
// buildSetBuilderData. A failure on one song doesn't stop the rest; the
// summary reports counts for each step plus what failed.
func (sc *SchedulingController) PublishSet(c *fiber.Ctx) error {
	ctx := middleware.RequestContext(c)
	orgID := middleware.CurrentOrganizationID(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")

	set, err := sc.store.GetOrCreateSet(ctx, orgID, serviceTypeID, planID)
	if err != nil {
		return render(c, views.SetBuilderBody(sc.buildSetBuilderDataWithErr(ctx, orgID, serviceTypeID, planID, err.Error())))
	}

	setSongs, err := sc.store.ListSetSongs(ctx, set.ID)
	if err != nil {
		return render(c, views.SetBuilderBody(sc.buildSetBuilderDataWithErr(ctx, orgID, serviceTypeID, planID, err.Error())))
	}

	pendingDeletions, err := sc.store.ListPendingDeletions(ctx, set.ID)
	if err != nil {
		return render(c, views.SetBuilderBody(sc.buildSetBuilderDataWithErr(ctx, orgID, serviceTypeID, planID, err.Error())))
	}

	// failures is appended from concurrent goroutines below (the deletion
	// fan-out and the create/update fan-out further down), guarded by
	// failuresMu throughout - a plain slice append isn't safe for
	// concurrent writers on its own. deleted/created/updated are tallied
	// via atomic counters for the same reason, converted back to plain
	// ints once both fan-outs have fully returned (RunConcurrently blocks
	// until every goroutine finishes, so deletions always finish
	// completely before any create/update starts - the same ordering the
	// sequential version had, and what "1. Deletions... before creating or
	// reordering anything else" below still means).
	var failures []string
	var failuresMu sync.Mutex
	var deletedCount, createdCount, updatedCount int32
	deleted, created, updated := 0, 0, 0

	songs, _ := fetchActiveSongs(middleware.RequestContext(c), orgID)
	titles := songTitleMap(songs)
	// Backfill titles for any already-scheduled song (setSongs or
	// pendingDeletions) the active-only fetch above excluded because it's
	// since been archived in PCO - see resolveMissingTitles' doc comment.
	// Titles here feed real writes (CreateItem's title on first publish),
	// not just display, so this matters more than the read-only builder
	// page's own version of the same backfill.
	scheduledSongIDs := make([]string, 0, len(setSongs)+len(pendingDeletions))
	for _, ss := range setSongs {
		scheduledSongIDs = append(scheduledSongIDs, ss.SongID)
	}
	for _, pd := range pendingDeletions {
		scheduledSongIDs = append(scheduledSongIDs, pd.SongID)
	}
	resolveMissingTitles(ctx, titles, scheduledSongIDs)
	// Best-effort, same tolerance as everywhere else this cache is read -
	// a miss here just means a newly-created item skips the Description/
	// Length/ArrangementID defaults below rather than blocking the
	// publish, and a fresh song that hasn't been through Song Info sync
	// yet still gets a live backfill attempt per-song right where it's
	// actually needed (see the create loop below), not just left blank
	// until the next manual sync.
	arrangementCache, _ := sc.store.SongArrangementInfo(ctx, orgID)

	// vocalCategoryID/leaderNames back syncLeaderNote's per-song write
	// below - resolved once here, not per song. vocalCategoryID's own
	// lookup is a local Postgres read (scheduling.Store.
	// VocalNoteCategoryID), not a PCO call; leaderNames batches every
	// distinct LeaderPersonID in this set into one where[id]=... call
	// (personNameMapForIDs), same batching precedent as titles/roles
	// above - empty when no song in this set has a leader set, in which
	// case the call is skipped entirely (see personLookupMapForIDs).
	vocalCategoryID, _ := sc.store.VocalNoteCategoryID(ctx, orgID, serviceTypeID)
	leaderNames := personNameMapForIDs(ctx, distinctLeaderPersonIDs(setSongs))

	// 1. Deletions - clear out what was removed/replaced locally before
	// creating or reordering anything else.
	scheduling.RunConcurrently(pendingDeletions, func(ss scheduling.SetSong) {
		title := titleOrID(titles, ss.SongID)

		if err := pco.DeleteItem(ctx, serviceTypeID, planID, ss.PCOItemID); err != nil {
			failuresMu.Lock()
			failures = append(failures, fmt.Sprintf("removing %s: %s", title, friendlyPCOErr(err)))
			failuresMu.Unlock()
			return
		}
		if err := sc.store.DeleteSetSongRow(ctx, ss.ID); err != nil {
			failuresMu.Lock()
			failures = append(failures, fmt.Sprintf("removed %s from Planning Center but failed to clear it locally: %s", title, err.Error()))
			failuresMu.Unlock()
			return
		}
		atomic.AddInt32(&deletedCount, 1)
	})
	deleted = int(deletedCount)

	// 2. Creates, then a single reorder call covering the whole plan - see
	// mergeServiceOrder for how each song's anchor places it relative to
	// the plan's other (foreign) items, the same merge the builder page
	// displays. reorderSucceeded tracks whether it's now safe to clear
	// every SetSong.Moved flag in this set (see ClearMovedFlags) -
	// anything short of an actual, confirmed pco.ReorderItems success (or
	// nothing needing reordering at all) leaves them set, so a failed
	// attempt still shows "Discard changes" for the next try instead of
	// quietly claiming to be in sync.
	reorderSucceeded := true
	if len(setSongs) > 0 {
		// Include: item_notes only when the leader sync is actually
		// configured (vocalCategoryID != "") - sideloading onto this
		// already-happening call (see itemNotesFromIncluded) is free
		// PCO-call-wise either way, but there's no reason to ask Planning
		// Center to bundle note data nobody here is about to look at.
		itemsParams := &pco.ItemsParams{}
		if vocalCategoryID != "" {
			itemsParams.Include = []string{"item_notes"}
		}
		existingItems, err := pco.GetItems(ctx, serviceTypeID, planID, itemsParams)
		if err != nil {
			failures = append(failures, "loading existing plan items from Planning Center: "+friendlyPCOErr(err))
			reorderSucceeded = false
		} else {
			sort.Slice(existingItems.Data, func(i, j int) bool {
				return existingItems.Data[i].Attributes.Sequence < existingItems.Data[j].Attributes.Sequence
			})
			notesByItem := itemNotesFromIncluded(existingItems.Included)

			nextSequence := 0
			for _, item := range existingItems.Data {
				if item.Attributes.Sequence >= nextSequence {
					nextSequence = item.Attributes.Sequence + 1
				}
			}

			// publishable ends up holding every song that has (or now
			// has) a real pco_item_id, for the merge below - one that
			// fails to create is left out, since there's no item id to
			// place in the reorder. Each setSongs[i] is independent of
			// every other's create/update call, so this fans out via
			// scheduling.RunConcurrently instead of one call at a time -
			// results is pre-sized and index-matched to setSongs (each
			// goroutine writes only its own index, no lock needed for
			// that part), with include=false meaning "leave this one out
			// of publishable" (mirroring the sequential version's
			// continue on a failed create - see mergeServiceOrder's own
			// doc comment for why publishable's list order here doesn't
			// need to match setSongs' order: it groups by each SetSong's
			// stored AnchorItemID/Position, not by input order).
			type publishResult struct {
				ss      scheduling.SetSong
				include bool
			}
			results := make([]publishResult, len(setSongs))
			indexes := make([]int, len(setSongs))
			for i := range indexes {
				indexes[i] = i
			}

			scheduling.RunConcurrently(indexes, func(i int) {
				ss := setSongs[i]
				title := titleOrID(titles, ss.SongID)

				if ss.PCOItemID == "" {
					// Description/Length/ArrangementID/KeyID all come from
					// the song's own cached arrangement, backfilling live
					// on a cache miss (fetchAndCacheArrangementInfo, same
					// cache-miss path resolveArrangementInfo uses for
					// display) - a song published for the first time
					// shouldn't have to wait for a separate Song Info sync
					// just to get its arrangement actually linked. Without
					// ArrangementID, PCO shows nothing but the bare title
					// for this item - no chord chart, lyrics, or structure
					// (confirmed live) - unlike a song added through
					// Planning Center's own UI; KeyID is a separate
					// relationship again (confirmed live: linking the
					// arrangement alone still leaves the item's key blank),
					// and stays empty when the song's key came from the
					// arrangement's ChordChartKey fallback rather than a
					// real Key sub-resource - there's nothing to link in
					// that case, just a display string.
					// info.ArrangementID == "" counts as a miss too, not just
					// !ok - real bug, caught after shipping: a song cached
					// before ArrangementID/KeyID existed as columns has a
					// row that's present but permanently blank on both,
					// and nothing ever re-fetched it since "cached" only
					// checked presence. Any library synced before this
					// session's arrangement-linking work landed would
					// otherwise never get it, publish after publish.
					info, ok := arrangementCache[ss.SongID]
					if !ok || info.ArrangementID == "" {
						if fetched, err := fetchAndCacheArrangementInfo(ctx, sc.store, orgID, ss.SongID); err == nil {
							info = fetched
						}
					}

					keyID, keyErr := resolveKeyID(ctx, ss.SongID, info.ArrangementID, ss.KeyOverride, info.KeyID)
					item, err := pco.CreateItem(ctx, serviceTypeID, planID, &pco.CreateItemParams{
						Title:         title,
						ItemType:      pco.ItemTypeSong,
						Sequence:      nextSequence + i,
						SongID:        ss.SongID,
						ArrangementID: info.ArrangementID,
						KeyID:         keyID,
						Description:   itemDescriptionFor(ss, info),
						Length:        info.LengthSeconds,
					})
					if err != nil {
						failuresMu.Lock()
						failures = append(failures, fmt.Sprintf("%s: %s", title, friendlyPCOErr(err)))
						failuresMu.Unlock()
						return
					}
					if err := sc.store.MarkSetSongPublished(ctx, set.ID, ss.ID, item.Data.ID); err != nil {
						failuresMu.Lock()
						failures = append(failures, fmt.Sprintf("%s: created in Planning Center but failed to record locally: %s", title, err.Error()))
						failuresMu.Unlock()
						return
					}
					// A KeyOverride that couldn't actually be resolved to a
					// real PCO key (see resolveKeyID) doesn't block the
					// create - the item still exists with the arrangement's
					// default key - but it must not look like a clean
					// success either, or the wrong key silently ships with
					// no record anyone would ever see it.
					if keyErr != nil {
						failuresMu.Lock()
						failures = append(failures, fmt.Sprintf("%s: created, but couldn't set key to %q - kept the arrangement's key instead: %s", title, ss.KeyOverride, friendlyPCOErr(keyErr)))
						failuresMu.Unlock()
					}
					// Best-effort, same tolerance as ClearMovedFlags below -
					// whatever Description/KeyOverride this create just
					// pushed is now genuinely in sync, whether or not this
					// call happens to succeed.
					_ = sc.store.ClearInfoPending(ctx, set.ID, ss.ID)
					ss.PCOItemID = item.Data.ID
					// A freshly-created item has no existing note in any
					// category yet ("", "" - nothing to preserve), so this
					// only ever creates a fresh note, never updates/deletes
					// one - see syncLeaderNote's own doc comment.
					if err := syncLeaderNote(ctx, serviceTypeID, planID, item.Data.ID, vocalCategoryID, "", "", titleOrID(leaderNames, ss.LeaderPersonID)); err != nil {
						failuresMu.Lock()
						failures = append(failures, fmt.Sprintf("%s: created, but couldn't sync the song leader note: %s", title, friendlyPCOErr(err)))
						failuresMu.Unlock()
					}
					atomic.AddInt32(&createdCount, 1)
				} else {
					// Already published - nothing to create, but a pending
					// note/key edit (see SetSong.InfoPending) still needs to
					// reach Planning Center, and Publish is the one action a
					// user reasonably expects to "sync everything," not just
					// creates/deletes/reorders. Skipped entirely when
					// nothing's pending, so a plain reorder-only publish
					// doesn't cost an extra PCO call per already-in-sync song.
					if ss.InfoPending {
						info, ok := arrangementCache[ss.SongID]
						if !ok || info.ArrangementID == "" {
							if fetched, err := fetchAndCacheArrangementInfo(ctx, sc.store, orgID, ss.SongID); err == nil {
								info = fetched
							}
						}
						length := info.LengthSeconds
						description := itemDescriptionFor(ss, info)
						keyID, keyErr := resolveKeyID(ctx, ss.SongID, info.ArrangementID, ss.KeyOverride, info.KeyID)
						// Deliberately no ArrangementID here, unlike
						// RefreshSongInfo's own UpdateItem call below - this
						// fires as a side effect of pushing a merely pending
						// Description/KeyOverride edit (see InfoPending
						// above), not because anyone asked to fix this
						// item's arrangement. info.ArrangementID is
						// song_arrangement_cache's single org-wide cached
						// arrangement (fetchAndCacheArrangementInfo's
						// GetArrangements(PerPage:1) - whichever one
						// Planning Center happens to return first for the
						// song), which is only ever correct by luck once a
						// song has more than one arrangement. Sending it
						// here would silently re-point an already-linked
						// item at that arrangement as a side effect of
						// saving a one-time key/note - exactly the kind of
						// surprise RefreshSongInfo exists to make an
						// explicit, opted-into action instead.
						if _, err := pco.UpdateItem(ctx, serviceTypeID, planID, ss.PCOItemID, &pco.UpdateItemParams{
							Description: &description,
							Length:      &length,
							KeyID:       &keyID,
						}); err != nil {
							failuresMu.Lock()
							failures = append(failures, fmt.Sprintf("%s: %s", title, friendlyPCOErr(err)))
							failuresMu.Unlock()
						} else {
							_ = sc.store.ClearInfoPending(ctx, set.ID, ss.ID)
							if keyErr != nil {
								failuresMu.Lock()
								failures = append(failures, fmt.Sprintf("%s: note/length updated, but couldn't set key to %q - kept the arrangement's key instead: %s", title, ss.KeyOverride, friendlyPCOErr(keyErr)))
								failuresMu.Unlock()
							}
							// currentNoteID/currentContent come from notesByItem
							// - sideloaded on the existingItems fetch above
							// (Include: item_notes), never a fresh per-song
							// call - see syncLeaderNote's own doc comment.
							currentNoteID, currentContent := noteInCategory(notesByItem, ss.PCOItemID, vocalCategoryID)
							if err := syncLeaderNote(ctx, serviceTypeID, planID, ss.PCOItemID, vocalCategoryID, currentNoteID, currentContent, titleOrID(leaderNames, ss.LeaderPersonID)); err != nil {
								failuresMu.Lock()
								failures = append(failures, fmt.Sprintf("%s: note/key updated, but couldn't sync the song leader note: %s", title, friendlyPCOErr(err)))
								failuresMu.Unlock()
							}
						}
					}
					atomic.AddInt32(&updatedCount, 1)
				}
				// results[i] is this goroutine's own, exclusive index into
				// a pre-sized slice - no lock needed (see RunConcurrently's
				// doc comment). A failed create returned above without
				// reaching here, so its zero-value publishResult
				// (include=false) is what excludes it from publishable
				// below, same as the sequential version's continue.
				results[i] = publishResult{ss: ss, include: true}
			})

			var publishable []scheduling.SetSong
			for _, r := range results {
				if r.include {
					publishable = append(publishable, r.ss)
				}
			}
			created, updated = int(createdCount), int(updatedCount)

			if len(publishable) > 0 {
				merged := mergeServiceOrder(existingItems.Data, publishable)
				finalOrder := make([]string, 0, len(merged))
				for _, row := range merged {
					if row.Item != nil {
						finalOrder = append(finalOrder, row.Item.ID)
					} else {
						finalOrder = append(finalOrder, row.Song.PCOItemID)
					}
				}

				if err := pco.ReorderItems(ctx, serviceTypeID, planID, finalOrder); err != nil {
					failures = append(failures, "reordering plan items: "+friendlyPCOErr(err))
					updated = 0 // the reorder that would have applied this failed
					reorderSucceeded = false
				}
			}
		}
	}
	if reorderSucceeded {
		// Best-effort - this is a local UI hint (whether "Discard changes"
		// needs to show), not data that was actually sent to Planning
		// Center, so a failure here doesn't belong in failures/errMsg.
		_ = sc.store.ClearMovedFlags(ctx, set.ID)
	}

	var summary []string
	if created > 0 {
		summary = append(summary, fmt.Sprintf("added %d", created))
	}
	if updated > 0 {
		summary = append(summary, fmt.Sprintf("reordered %d", updated))
	}
	if deleted > 0 {
		summary = append(summary, fmt.Sprintf("removed %d", deleted))
	}

	publishMsg := ""
	if len(summary) > 0 {
		publishMsg = "Synced to Planning Center: " + strings.Join(summary, ", ") + "."
	} else if len(failures) == 0 {
		publishMsg = "Nothing to publish - already in sync with Planning Center."
	}

	errMsg := ""
	if len(failures) > 0 {
		errMsg = "Failed to sync: " + strings.Join(failures, "; ")
	}

	data := sc.buildSetBuilderData(ctx, orgID, serviceTypeID, planID)
	data.PublishMsg = publishMsg
	if errMsg != "" {
		data.ErrMsg = appendErr(errMsg, data.ErrMsg)
	}

	return render(c, views.SetBuilderBody(data))
}

// RefreshSongInfo re-applies every already-published song's current
// arrangement info (Description/Length/ArrangementID/KeyID) to its
// existing plan item, via a plain PATCH per song - the low-cost
// alternative to Discard-and-republish for when that info was wrong or
// missing at the time it was originally published. PublishSet only ever
// sets these once, at creation time (same as Title/ItemType/Sequence/
// SongID) - it never goes back and fixes an already-published item, so
// without this, correcting a stale/wrong item meant discarding the whole
// local set and rebuilding it from scratch. Never touches Title/ItemType/
// Sequence/SongID, and never creates, deletes, or reorders anything - a
// targeted PATCH only, using the same cache-or-live-backfill resolution
// PublishSet's own create path uses.
func (sc *SchedulingController) RefreshSongInfo(c *fiber.Ctx) error {
	ctx := middleware.RequestContext(c)
	orgID := middleware.CurrentOrganizationID(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")

	set, err := sc.store.GetOrCreateSet(ctx, orgID, serviceTypeID, planID)
	if err != nil {
		return render(c, views.SetBuilderBody(sc.buildSetBuilderDataWithErr(ctx, orgID, serviceTypeID, planID, err.Error())))
	}

	setSongs, err := sc.store.ListSetSongs(ctx, set.ID)
	if err != nil {
		return render(c, views.SetBuilderBody(sc.buildSetBuilderDataWithErr(ctx, orgID, serviceTypeID, planID, err.Error())))
	}

	songs, _ := fetchActiveSongs(ctx, orgID)
	titles := songTitleMap(songs)
	// See PublishSet's identical backfill for why - setSongs here can
	// equally reference a song that's since been archived in PCO.
	setSongIDs := make([]string, 0, len(setSongs))
	for _, ss := range setSongs {
		setSongIDs = append(setSongIDs, ss.SongID)
	}
	resolveMissingTitles(ctx, titles, setSongIDs)
	arrangementCache, _ := sc.store.SongArrangementInfo(ctx, orgID)

	// vocalCategoryID/leaderNames/notesByItem back syncLeaderNote's
	// per-song write below - see PublishSet's identical setup for the
	// full reasoning. Unlike PublishSet (which already fetches plan items
	// for the reorder step regardless), RefreshSongInfo otherwise never
	// calls GetItems at all - so the one extra GetItems(Include:
	// item_notes) call here only happens when this service type has
	// actually configured the vocal notes category setting
	// (vocalCategoryID != ""), never as a cost every org pays by default.
	// See docs/pco-api-risks.md's Item Notes entry.
	vocalCategoryID, _ := sc.store.VocalNoteCategoryID(ctx, orgID, serviceTypeID)
	leaderNames := personNameMapForIDs(ctx, distinctLeaderPersonIDs(setSongs))
	var notesByItem map[string][]pco.ItemNoteData
	if vocalCategoryID != "" {
		if items, err := pco.GetItems(ctx, serviceTypeID, planID, &pco.ItemsParams{Include: []string{"item_notes"}}); err == nil {
			notesByItem = itemNotesFromIncluded(items.Included)
		}
	}

	// Every song's PATCH is independent of every other's (never creates,
	// deletes, or reorders anything - see this function's own doc
	// comment), so fanned out via scheduling.RunConcurrently instead of
	// one call at a time. failures/refreshedCount follow the same
	// mutex/atomic pattern as PublishSet's own fan-outs above.
	var failures []string
	var failuresMu sync.Mutex
	var refreshedCount int32

	toRefresh := make([]scheduling.SetSong, 0, len(setSongs))
	for _, ss := range setSongs {
		if ss.PCOItemID != "" {
			toRefresh = append(toRefresh, ss)
		}
	}

	scheduling.RunConcurrently(toRefresh, func(ss scheduling.SetSong) {
		title := titleOrID(titles, ss.SongID)

		info, ok := arrangementCache[ss.SongID]
		if !ok || info.ArrangementID == "" {
			if fetched, err := fetchAndCacheArrangementInfo(ctx, sc.store, orgID, ss.SongID); err == nil {
				info = fetched
			}
		}

		length := info.LengthSeconds
		description := itemDescriptionFor(ss, info)
		keyID, keyErr := resolveKeyID(ctx, ss.SongID, info.ArrangementID, ss.KeyOverride, info.KeyID)
		if _, err := pco.UpdateItem(ctx, serviceTypeID, planID, ss.PCOItemID, &pco.UpdateItemParams{
			Description:   &description,
			Length:        &length,
			ArrangementID: &info.ArrangementID,
			KeyID:         &keyID,
		}); err != nil {
			failuresMu.Lock()
			failures = append(failures, fmt.Sprintf("%s: %s", title, err.Error()))
			failuresMu.Unlock()
			return
		}
		// Best-effort, same as ClearInfoPending's other call site (PublishSet) -
		// this song's Description/KeyOverride genuinely just reached
		// Planning Center, whether or not this particular call succeeds.
		_ = sc.store.ClearInfoPending(ctx, set.ID, ss.ID)
		if keyErr != nil {
			failuresMu.Lock()
			failures = append(failures, fmt.Sprintf("%s: refreshed, but couldn't set key to %q - kept the arrangement's key instead: %s", title, ss.KeyOverride, friendlyPCOErr(keyErr)))
			failuresMu.Unlock()
		}
		// Re-applied for every already-published song here, regardless of
		// InfoPending - same "force resync" contract as the
		// Description/Length/ArrangementID/KeyID PATCH just above, unlike
		// PublishSet's own update branch (which only pushes when
		// something's actually pending).
		currentNoteID, currentContent := noteInCategory(notesByItem, ss.PCOItemID, vocalCategoryID)
		if err := syncLeaderNote(ctx, serviceTypeID, planID, ss.PCOItemID, vocalCategoryID, currentNoteID, currentContent, titleOrID(leaderNames, ss.LeaderPersonID)); err != nil {
			failuresMu.Lock()
			failures = append(failures, fmt.Sprintf("%s: refreshed, but couldn't sync the song leader note: %s", title, friendlyPCOErr(err)))
			failuresMu.Unlock()
		}
		atomic.AddInt32(&refreshedCount, 1)
	})
	refreshed := int(refreshedCount)

	publishMsg := fmt.Sprintf("Refreshed %d song(s).", refreshed)
	if refreshed == 0 && len(failures) == 0 {
		publishMsg = "Nothing to refresh - no published songs in this set yet."
	}

	errMsg := ""
	if len(failures) > 0 {
		errMsg = "Failed to refresh: " + strings.Join(failures, "; ")
	}

	data := sc.buildSetBuilderData(ctx, orgID, serviceTypeID, planID)
	data.PublishMsg = publishMsg
	if errMsg != "" {
		data.ErrMsg = appendErr(errMsg, data.ErrMsg)
	}

	return render(c, views.SetBuilderBody(data))
}

// trackedItemChange is one detected difference between an already-tracked,
// active (non-tombstoned) set_songs row and its live Planning Center item -
// see reconcileTrackedItems, MergeSet's expanded reconciliation (step 3 of
// docs/plans/in-progress/live-sync-reflection-plan.md). Purely a report;
// nothing here mutates anything itself.
type trackedItemChange struct {
	RowID int64
	Title string
	// Missing is true when PCOItemID no longer matches any live item - a
	// PCO-side deletion race. MergeSet responds by clearing PCOItemID back
	// to "" (see scheduling.Store.ClearSetSongPCOItemID) rather than
	// deleting the local row, so the song stays here, just unpublished
	// again. NewDescription/NewKeyOverride are meaningless when this is
	// true.
	Missing bool
	// NewDescription/NewKeyOverride are PCO's current Description/KeyName
	// to adopt locally - only ever populated for a row whose InfoPending is
	// false, so a pending local edit is never overwritten (see the
	// conflict-policy decision the plan doc settled: a local pending edit
	// always wins).
	NewDescription string
	NewKeyOverride string
}

// reconcileTrackedItems is MergeSet's step-3 core: given the plan's live
// items and this set's already-tracked, active set_songs rows (setSongs -
// pendingDeletions are out of scope, same as everywhere else in this file),
// it reports what's changed in Planning Center since this app last saw
// each row, without mutating anything itself - the caller (MergeSet) is
// what actually applies these via the store.
//
// A row with InfoPending true - an unpushed local Description/KeyOverride
// edit - is skipped entirely: the local edit always wins, never reconciled
// against PCO here, per the plan doc's conflict-policy decision. A row
// whose PCOItemID no longer matches any live item is reported Missing
// (see trackedItemChange) rather than compared field-by-field. Everything
// else is compared against PCO's live Description/KeyName, and only
// reported when at least one actually differs.
func reconcileTrackedItems(items []pco.ItemData, setSongs []scheduling.SetSong) []trackedItemChange {
	byID := make(map[string]pco.ItemData, len(items))
	for _, item := range items {
		byID[item.ID] = item
	}

	var changes []trackedItemChange
	for _, ss := range setSongs {
		if ss.PCOItemID == "" {
			continue
		}

		item, ok := byID[ss.PCOItemID]
		if !ok {
			changes = append(changes, trackedItemChange{RowID: ss.ID, Missing: true})
			continue
		}
		if ss.InfoPending {
			continue
		}
		if item.Attributes.Description != ss.Description || item.Attributes.KeyName != ss.KeyOverride {
			changes = append(changes, trackedItemChange{
				RowID:          ss.ID,
				NewDescription: item.Attributes.Description,
				NewKeyOverride: item.Attributes.KeyName,
			})
		}
	}
	return changes
}

// trackedItemOrderChanged reports whether the relative order of this set's
// already-tracked, published songs' PCO items - as PCO's own Sequence
// attribute has them right now - still matches the order this app's local
// anchor chain implies once merged with the plan's other (foreign) items
// (see mergeServiceOrder). A mismatch means someone reordered directly in
// Planning Center since this app last synced. Per the plan doc's
// order-changed decision, MergeSet only ever surfaces this as a plain
// warning - it never attempts an automatic reorder reconciliation, and
// never touches any SetSong's Position/AnchorItemID.
//
// items must already be sorted by Sequence (every caller in this file
// already sorts before use). expected is mergeServiceOrder's own walk,
// filtered to just this set's published song rows in the order it placed
// them - which, so long as nothing has drifted, is exactly the same order
// their PCO items' own live Sequence puts them in; live is that same
// tracked-id set, ordered purely by each item's current Sequence. Any
// difference between the two orderings means a real reorder happened.
func trackedItemOrderChanged(items []pco.ItemData, setSongs []scheduling.SetSong) bool {
	merged := mergeServiceOrder(items, setSongs)

	var expected []string
	tracked := make(map[string]bool)
	for _, row := range merged {
		if row.Song != nil && row.Song.PCOItemID != "" {
			expected = append(expected, row.Song.PCOItemID)
			tracked[row.Song.PCOItemID] = true
		}
	}

	var live []string
	for _, item := range items {
		if tracked[item.ID] {
			live = append(live, item.ID)
		}
	}

	if len(expected) != len(live) {
		return true
	}
	for i := range expected {
		if expected[i] != live[i] {
			return true
		}
	}
	return false
}

// MergeSet is the non-destructive third sync direction, alongside
// PublishSet ("assert what's here") and RevertSet ("conform to what's
// there," discarding local changes): it pulls in anything that changed
// directly in Planning Center since the local draft was last touched,
// WITHOUT discarding or altering any competing local edit. This covers two
// distinct cases now, not just one:
//
//   - Net-new items: walks the plan's items the same way RevertSet's
//     deriveImportRows does (see deriveMergeImportRows), inserting a fresh
//     local row for any song-linked item whose PCO item id isn't already
//     tracked by an existing set_songs row here (active or tombstoned).
//   - Already-tracked items (reconcileTrackedItems, above): a row whose PCO
//     item's Description/KeyName changed gets updated to match, but only
//     when InfoPending is false - a pending local edit always wins and is
//     left completely untouched (the field-conflict policy the plan doc
//     settled). A row whose PCO item was deleted out from under it has its
//     PCOItemID cleared back to "" (re-publishable, not deleted locally) -
//     see scheduling.Store.ClearSetSongPCOItemID. A pure reorder among
//     tracked items (trackedItemOrderChanged, above) is surfaced as a
//     plain warning, never auto-corrected.
//
// Like RevertSet, this never calls a PCO write endpoint - it's a pure
// local reconciliation from a fresh read.
func (sc *SchedulingController) MergeSet(c *fiber.Ctx) error {
	ctx := middleware.RequestContext(c)
	orgID := middleware.CurrentOrganizationID(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")

	set, err := sc.store.GetOrCreateSet(ctx, orgID, serviceTypeID, planID)
	errMsg := ""
	var summary []string
	if err != nil {
		errMsg = err.Error()
	} else {
		setSongs, err := sc.store.ListSetSongs(ctx, set.ID)
		if err != nil {
			errMsg = err.Error()
		}
		pendingDeletions, pdErr := sc.store.ListPendingDeletions(ctx, set.ID)
		if pdErr != nil {
			errMsg = appendErr(errMsg, pdErr.Error())
		}

		if errMsg == "" {
			trackedItemIDs := trackedPCOItemIDs(setSongs, pendingDeletions)

			items, itemsErr := pco.GetItems(ctx, serviceTypeID, planID, nil)
			if itemsErr != nil {
				errMsg = "loading plan items from Planning Center: " + itemsErr.Error()
			} else {
				sort.Slice(items.Data, func(i, j int) bool {
					return items.Data[i].Attributes.Sequence < items.Data[j].Attributes.Sequence
				})

				importRows := deriveMergeImportRows(items.Data, trackedItemIDs)
				imported, importErr := sc.store.ImportNewSongsFromPCO(ctx, set.ID, importRows)
				if importErr != nil {
					errMsg = appendErr(errMsg, importErr.Error())
				} else if imported > 0 {
					summary = append(summary, fmt.Sprintf("imported %d new song(s)", imported))
				}

				// Order-changed check runs against setSongs as fetched
				// above - before any of the reconciliation writes below,
				// none of which touch Position/AnchorItemID anyway - so
				// this reads the same "did PCO's order drift out from
				// under our anchors" question either way.
				orderChanged := trackedItemOrderChanged(items.Data, setSongs)

				updated, cleared := 0, 0
				for _, change := range reconcileTrackedItems(items.Data, setSongs) {
					if change.Missing {
						if err := sc.store.ClearSetSongPCOItemID(ctx, set.ID, change.RowID); err != nil {
							errMsg = appendErr(errMsg, err.Error())
							continue
						}
						cleared++
						continue
					}
					if err := sc.store.SyncSetSongInfoFromPCO(ctx, set.ID, change.RowID, change.NewDescription, change.NewKeyOverride); err != nil {
						errMsg = appendErr(errMsg, err.Error())
						continue
					}
					updated++
				}
				if updated > 0 {
					summary = append(summary, fmt.Sprintf("updated %d song(s) from Planning Center", updated))
				}
				if cleared > 0 {
					summary = append(summary, fmt.Sprintf("cleared %d broken reference(s) removed in Planning Center", cleared))
				}
				if orderChanged {
					summary = append(summary, "song order also changed in Planning Center - review before publishing")
				}
			}
		}
	}

	publishMsg := ""
	if len(summary) > 0 {
		publishMsg = strings.Join(summary, "; ") + "."
		publishMsg = strings.ToUpper(publishMsg[:1]) + publishMsg[1:]
	} else if errMsg == "" {
		publishMsg = "Nothing new to pull in - already up to date."
	}

	data := sc.buildSetBuilderData(ctx, orgID, serviceTypeID, planID)
	data.PublishMsg = publishMsg
	if errMsg != "" {
		data.ErrMsg = appendErr(errMsg, data.ErrMsg)
	}

	return render(c, views.SetBuilderBody(data))
}

// SyncCheck is the poll-while-open live-sync check's endpoint
// (docs/plans/in-progress/live-sync-reflection-plan.md, option C): polled
// every 30s by the set builder page's own hidden trigger element
// (set_builder.templ) while a Build page's Songs tab is open, carrying the
// fingerprint (see SetBuilderData.PCOSyncFingerprint/pcoItemsFingerprint)
// the page last saw as the fp query param. Does its own GetItems call
// (there's no cheaper "has this plan changed" PCO endpoint - live-verified
// against the dev org that neither Plan.UpdatedAt nor Plan.ItemsCount
// reflect item-level edits or pure reorders, see pco-api-risks.md),
// recomputes the same fingerprint, and compares:
//
//   - Equal: nothing changed since fp was captured - an empty 200, so htmx
//     (hx-swap="none" on the polling element) does nothing at all.
//   - Different: something changed - responds with exactly two
//     hx-swap-oob="true" fragments, matching set_builder.templ's own ids:
//     the banner slot (#pco-sync-banner), populated with an ImportBanner
//     offering to run the existing MergeSet endpoint, and the hidden
//     fingerprint input (#pco-sync-fp), updated to the fresh value so the
//     next poll tick doesn't re-show the banner for the same
//     already-detected change.
//
// Tagged pco.WithPriority(ctx, 20) - same convention as the background
// syncs in person_data_cache.go/song_arrangement_cache.go/jobs/pco.go - so
// an unattended, timer-driven poll never delays interactive traffic
// sharing the same token. A failed GetItems call (a transient PCO error,
// or a 429 the retry backstop didn't absorb) is treated the same as "no
// change detected" - an empty 200 - rather than surfacing an error banner
// for what's very likely a fine plan on the next tick.
func (sc *SchedulingController) SyncCheck(c *fiber.Ctx) error {
	ctx := pco.WithPriority(middleware.RequestContext(c), 20)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")
	clientFP := c.Query("fp")

	items, err := pco.GetItems(ctx, serviceTypeID, planID, nil)
	if err != nil {
		return c.SendStatus(fiber.StatusOK)
	}

	freshFP := pcoItemsFingerprint(items.Data)
	if freshFP == clientFP {
		return c.SendStatus(fiber.StatusOK)
	}

	return render(c, views.SyncCheckBanner(serviceTypeID, planID, freshFP))
}

// RevertSet is PublishSet's opposite: instead of pushing local changes to
// Planning Center, it discards them and rebuilds the local set from
// whatever Planning Center's plan currently has, so it's back in sync.
// Song-linked items become local songs; every other item becomes the
// anchor for whichever tracked songs immediately follow it, mirroring the
// same walk mergeServiceOrder does in reverse. This never calls a PCO
// write endpoint - it's a pure local reset from a fresh read.
func (sc *SchedulingController) RevertSet(c *fiber.Ctx) error {
	ctx := middleware.RequestContext(c)
	orgID := middleware.CurrentOrganizationID(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")

	set, err := sc.store.GetOrCreateSet(ctx, orgID, serviceTypeID, planID)
	errMsg := ""
	publishMsg := ""
	if err != nil {
		errMsg = err.Error()
	} else {
		items, err := pco.GetItems(ctx, serviceTypeID, planID, nil)
		if err != nil {
			errMsg = "loading plan items from Planning Center: " + err.Error()
		} else {
			sort.Slice(items.Data, func(i, j int) bool {
				return items.Data[i].Attributes.Sequence < items.Data[j].Attributes.Sequence
			})

			importRows := deriveImportRows(items.Data)

			if err := sc.store.RevertSet(ctx, set.ID, importRows); err != nil {
				errMsg = err.Error()
			} else {
				publishMsg = "Reverted to match Planning Center."
			}
		}
	}

	data := sc.buildSetBuilderData(ctx, orgID, serviceTypeID, planID)
	data.PublishMsg = publishMsg
	if errMsg != "" {
		data.ErrMsg = appendErr(errMsg, data.ErrMsg)
	}

	return render(c, views.SetBuilderBody(data))
}
