package controllers

// Publish/Refresh/Revert: syncing the draft set to and from real Planning
// Center plan items.

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"pco-assistant/internal/scheduling"
	"pco-assistant/internal/server/middleware"
	views "pco-assistant/internal/views/scheduling"

	pco "github.com/drkgrntt/go-pco"
	"github.com/gofiber/fiber/v2"
)

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

	var failures []string
	deleted, created, updated := 0, 0, 0

	songs, _ := fetchAllSongs(middleware.RequestContext(c))
	titles := songTitleMap(songs)
	// Best-effort, same tolerance as everywhere else this cache is read -
	// a miss here just means a newly-created item skips the Description/
	// Length/ArrangementID defaults below rather than blocking the
	// publish, and a fresh song that hasn't been through Song Info sync
	// yet still gets a live backfill attempt per-song right where it's
	// actually needed (see the create loop below), not just left blank
	// until the next manual sync.
	arrangementCache, _ := sc.store.SongArrangementInfo(ctx, orgID)

	// 1. Deletions - clear out what was removed/replaced locally before
	// creating or reordering anything else.
	for _, ss := range pendingDeletions {
		title := titleOrID(titles, ss.SongID)

		if err := pco.DeleteItem(ctx, serviceTypeID, planID, ss.PCOItemID); err != nil {
			failures = append(failures, fmt.Sprintf("removing %s: %s", title, err.Error()))
			continue
		}
		if err := sc.store.DeleteSetSongRow(ctx, ss.ID); err != nil {
			failures = append(failures, fmt.Sprintf("removed %s from Planning Center but failed to clear it locally: %s", title, err.Error()))
			continue
		}
		deleted++
	}

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
		existingItems, err := pco.GetItems(ctx, serviceTypeID, planID, nil)
		if err != nil {
			failures = append(failures, "loading existing plan items from Planning Center: "+err.Error())
			reorderSucceeded = false
		} else {
			sort.Slice(existingItems.Data, func(i, j int) bool {
				return existingItems.Data[i].Attributes.Sequence < existingItems.Data[j].Attributes.Sequence
			})

			nextSequence := 0
			for _, item := range existingItems.Data {
				if item.Attributes.Sequence >= nextSequence {
					nextSequence = item.Attributes.Sequence + 1
				}
			}

			// publishable ends up holding every song that has (or now
			// has) a real pco_item_id, for the merge below - one that
			// fails to create is left out, since there's no item id to
			// place in the reorder.
			var publishable []scheduling.SetSong
			for i, ss := range setSongs {
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

					item, err := pco.CreateItem(ctx, serviceTypeID, planID, &pco.CreateItemParams{
						Title:         title,
						ItemType:      pco.ItemTypeSong,
						Sequence:      nextSequence + i,
						SongID:        ss.SongID,
						ArrangementID: info.ArrangementID,
						KeyID:         resolveKeyID(ctx, ss.SongID, info.ArrangementID, ss.KeyOverride, info.KeyID),
						Description:   itemDescriptionFor(ss, info),
						Length:        info.LengthSeconds,
					})
					if err != nil {
						failures = append(failures, fmt.Sprintf("%s: %s", title, err.Error()))
						continue
					}
					if err := sc.store.MarkSetSongPublished(ctx, set.ID, ss.ID, item.Data.ID); err != nil {
						failures = append(failures, fmt.Sprintf("%s: created in Planning Center but failed to record locally: %s", title, err.Error()))
						continue
					}
					// Best-effort, same tolerance as ClearMovedFlags below -
					// whatever Description/KeyOverride this create just
					// pushed is now genuinely in sync, whether or not this
					// call happens to succeed.
					_ = sc.store.ClearInfoPending(ctx, set.ID, ss.ID)
					ss.PCOItemID = item.Data.ID
					created++
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
						keyID := resolveKeyID(ctx, ss.SongID, info.ArrangementID, ss.KeyOverride, info.KeyID)
						if _, err := pco.UpdateItem(ctx, serviceTypeID, planID, ss.PCOItemID, &pco.UpdateItemParams{
							Description:   &description,
							Length:        &length,
							ArrangementID: &info.ArrangementID,
							KeyID:         &keyID,
						}); err != nil {
							failures = append(failures, fmt.Sprintf("%s: %s", title, err.Error()))
						} else {
							_ = sc.store.ClearInfoPending(ctx, set.ID, ss.ID)
						}
					}
					updated++
				}
				publishable = append(publishable, ss)
			}

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
					failures = append(failures, "reordering plan items: "+err.Error())
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

	songs, _ := fetchAllSongs(ctx)
	titles := songTitleMap(songs)
	arrangementCache, _ := sc.store.SongArrangementInfo(ctx, orgID)

	var failures []string
	refreshed := 0
	for _, ss := range setSongs {
		if ss.PCOItemID == "" {
			continue
		}
		title := titleOrID(titles, ss.SongID)

		info, ok := arrangementCache[ss.SongID]
		if !ok || info.ArrangementID == "" {
			if fetched, err := fetchAndCacheArrangementInfo(ctx, sc.store, orgID, ss.SongID); err == nil {
				info = fetched
			}
		}

		length := info.LengthSeconds
		description := itemDescriptionFor(ss, info)
		keyID := resolveKeyID(ctx, ss.SongID, info.ArrangementID, ss.KeyOverride, info.KeyID)
		if _, err := pco.UpdateItem(ctx, serviceTypeID, planID, ss.PCOItemID, &pco.UpdateItemParams{
			Description:   &description,
			Length:        &length,
			ArrangementID: &info.ArrangementID,
			KeyID:         &keyID,
		}); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %s", title, err.Error()))
			continue
		}
		// Best-effort, same as ClearInfoPending's other call site (PublishSet) -
		// this song's Description/KeyOverride genuinely just reached
		// Planning Center, whether or not this particular call succeeds.
		_ = sc.store.ClearInfoPending(ctx, set.ID, ss.ID)
		refreshed++
	}

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
