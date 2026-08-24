package controllers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"pco-assistant/internal/scheduling"
	"pco-assistant/internal/server/middleware"
	views "pco-assistant/internal/views/scheduling"

	pco "github.com/drkgrntt/go-pco"
	"github.com/gofiber/fiber/v2"
)

// --- Song roles (opener / response tagging) -------------------------------

// songRoleTypeLabelMap builds a role code -> label lookup from
// ListSongRoleTypes, for resolving a tag's stored role code back to its
// display label (see SongRolesTable).
func songRoleTypeLabelMap(types []scheduling.SongRoleType) map[string]string {
	labels := make(map[string]string, len(types))
	for _, t := range types {
		labels[t.Code] = t.Label
	}
	return labels
}

func (sc *SchedulingController) SongRolesPage(c *fiber.Ctx) error {
	_, personName := middleware.CurrentPerson(c)
	orgName := middleware.CurrentOrganization(c)
	orgID := middleware.CurrentOrganizationID(c)

	songs, songsErr := fetchAllSongs(middleware.RequestContext(c))

	roleTypes, err := sc.store.ListSongRoleTypes(c.Context())
	if err != nil {
		return render(c, views.SongRoles(personName, orgName, songs, nil, nil, songTitleMap(songs), nil, err.Error()))
	}

	roles, err := sc.store.ListSongRoles(c.Context(), orgID, "", "")
	if err != nil {
		return render(c, views.SongRoles(personName, orgName, songs, nil, roleTypes, songTitleMap(songs), songRoleTypeLabelMap(roleTypes), err.Error()))
	}

	errMsg := ""
	if songsErr != nil {
		errMsg = "loading songs from Planning Center: " + songsErr.Error()
	}

	return render(c, views.SongRoles(personName, orgName, songs, roles, roleTypes, songTitleMap(songs), songRoleTypeLabelMap(roleTypes), errMsg))
}

func (sc *SchedulingController) CreateSongRole(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)

	var body scheduling.CreateSongRoleParams
	errMsg := ""
	if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else if id, editing, idErr := parseFormID(c); idErr != nil {
		errMsg = "id must be an integer"
	} else if editing {
		if _, err := sc.store.UpdateSongRole(c.Context(), orgID, id, body); err != nil {
			errMsg = err.Error()
		}
	} else if _, err := sc.store.CreateSongRole(c.Context(), orgID, body); err != nil {
		errMsg = err.Error()
	}

	roles, err := sc.store.ListSongRoles(c.Context(), orgID, "", "")
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}

	songs, _ := fetchAllSongs(middleware.RequestContext(c))
	roleTypes, _ := sc.store.ListSongRoleTypes(c.Context())

	return render(c, views.SongRolesTable(roles, songTitleMap(songs), songRoleTypeLabelMap(roleTypes), errMsg))
}

func (sc *SchedulingController) DeleteSongRole(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)

	id, err := c.ParamsInt("id")
	errMsg := ""
	if err != nil {
		errMsg = "id must be an integer"
	} else if delErr := sc.store.DeleteSongRole(c.Context(), orgID, int64(id)); delErr != nil && !errors.Is(delErr, sql.ErrNoRows) {
		errMsg = delErr.Error()
	}

	roles, err := sc.store.ListSongRoles(c.Context(), orgID, "", "")
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}

	songs, _ := fetchAllSongs(middleware.RequestContext(c))
	roleTypes, _ := sc.store.ListSongRoleTypes(c.Context())

	return render(c, views.SongRolesTable(roles, songTitleMap(songs), songRoleTypeLabelMap(roleTypes), errMsg))
}

// --- Set builder ------------------------------------------------------------

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

func (sc *SchedulingController) AddSetSong(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")

	var body struct {
		SongID string `form:"song_id"`
	}
	errMsg := ""
	if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else if body.SongID == "" {
		errMsg = "song_id is required"
	} else if set, err := sc.store.GetOrCreateSet(c.Context(), orgID, serviceTypeID, planID); err != nil {
		errMsg = err.Error()
	} else if _, err := sc.store.AddSongToSet(c.Context(), set.ID, body.SongID); err != nil {
		errMsg = err.Error()
	}

	return render(c, views.SetBuilderBody(sc.buildSetBuilderDataWithErr(middleware.RequestContext(c), orgID, serviceTypeID, planID, errMsg)))
}

func (sc *SchedulingController) RemoveSetSong(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")

	errMsg := ""
	if rowID, err := c.ParamsInt("set_song_id"); err != nil {
		errMsg = "set_song_id must be an integer"
	} else if set, err := sc.store.GetOrCreateSet(c.Context(), orgID, serviceTypeID, planID); err != nil {
		errMsg = err.Error()
	} else if err := sc.store.RemoveSongFromSet(c.Context(), set.ID, int64(rowID)); err != nil && !errors.Is(err, sql.ErrNoRows) {
		errMsg = err.Error()
	}

	return render(c, views.SetBuilderBody(sc.buildSetBuilderDataWithErr(middleware.RequestContext(c), orgID, serviceTypeID, planID, errMsg)))
}

// DuplicateSetSong creates a second occurrence of an existing set song
// (same library song, own independent row/position/publish state) right
// after it - for a song that's reprised elsewhere in the service, e.g. the
// same opener sung again as the response.
func (sc *SchedulingController) DuplicateSetSong(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")

	errMsg := ""
	if rowID, err := c.ParamsInt("set_song_id"); err != nil {
		errMsg = "set_song_id must be an integer"
	} else if set, err := sc.store.GetOrCreateSet(c.Context(), orgID, serviceTypeID, planID); err != nil {
		errMsg = err.Error()
	} else if _, err := sc.store.DuplicateSongInSet(c.Context(), set.ID, int64(rowID)); err != nil {
		errMsg = err.Error()
	}

	return render(c, views.SetBuilderBody(sc.buildSetBuilderDataWithErr(middleware.RequestContext(c), orgID, serviceTypeID, planID, errMsg)))
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
					item, err := pco.CreateItem(ctx, serviceTypeID, planID, &pco.CreateItemParams{
						Title:    title,
						ItemType: pco.ItemTypeSong,
						Sequence: nextSequence + i,
						SongID:   ss.SongID,
					})
					if err != nil {
						failures = append(failures, fmt.Sprintf("%s: %s", title, err.Error()))
						continue
					}
					if err := sc.store.MarkSetSongPublished(ctx, set.ID, ss.ID, item.Data.ID); err != nil {
						failures = append(failures, fmt.Sprintf("%s: created in Planning Center but failed to record locally: %s", title, err.Error()))
						continue
					}
					ss.PCOItemID = item.Data.ID
					created++
				} else {
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

// ReplaceSetSong swaps the set song identified in the URL for the posted
// song_id, in place - see scheduling.Store.ReplaceSongInSet.
func (sc *SchedulingController) ReplaceSetSong(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")

	var body struct {
		SongID string `form:"song_id"`
	}
	errMsg := ""
	rowID, rowErr := c.ParamsInt("set_song_id")
	if rowErr != nil {
		errMsg = "set_song_id must be an integer"
	} else if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else if body.SongID == "" {
		errMsg = "song_id is required"
	} else if set, err := sc.store.GetOrCreateSet(c.Context(), orgID, serviceTypeID, planID); err != nil {
		errMsg = err.Error()
	} else if _, err := sc.store.ReplaceSongInSet(c.Context(), set.ID, int64(rowID), body.SongID); err != nil {
		errMsg = err.Error()
	}

	return render(c, views.SetBuilderBody(sc.buildSetBuilderDataWithErr(middleware.RequestContext(c), orgID, serviceTypeID, planID, errMsg)))
}

// RepositionSetSong is what the order-of-service drag-and-drop calls: the
// dragged row is placed immediately after whatever row it was dropped on
// (?after=, either "item:<pco_item_id>" for a foreign row or "row:<id>" for
// another set song - see parseAfterToken and RepositionSongInSet).
func (sc *SchedulingController) RepositionSetSong(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")

	var body struct {
		After string `form:"after"`
	}
	errMsg := ""
	rowID, rowErr := c.ParamsInt("set_song_id")
	if rowErr != nil {
		errMsg = "set_song_id must be an integer"
	} else if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else {
		anchorItemID, afterRowID, parseErr := parseAfterToken(body.After)
		if parseErr != nil {
			errMsg = parseErr.Error()
		} else if set, err := sc.store.GetOrCreateSet(c.Context(), orgID, serviceTypeID, planID); err != nil {
			errMsg = err.Error()
		} else if err := sc.store.RepositionSongInSet(c.Context(), set.ID, int64(rowID), anchorItemID, afterRowID); err != nil {
			errMsg = err.Error()
		}
	}

	return render(c, views.SetBuilderBody(sc.buildSetBuilderDataWithErr(middleware.RequestContext(c), orgID, serviceTypeID, planID, errMsg)))
}

// parseAfterToken decodes the drag-and-drop "after" token: "" (top of
// service), "item:<id>" (right after a foreign item - the start of its
// group), or "row:<id>" (right after another set song's row, inheriting
// its anchor).
func parseAfterToken(token string) (anchorItemID string, afterRowID int64, err error) {
	if after, ok := strings.CutPrefix(token, "item:"); ok {
		return after, 0, nil
	}
	if after, ok := strings.CutPrefix(token, "row:"); ok {
		id, err := strconv.ParseInt(after, 10, 64)
		if err != nil {
			return "", 0, fmt.Errorf("invalid row id in after token %q", token)
		}
		return "", id, nil
	}
	return "", 0, nil
}

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

	openerRoles, _ := sc.store.ListSongRoles(ctx, organizationID, "", scheduling.RoleOpener)
	responseRoles, _ := sc.store.ListSongRoles(ctx, organizationID, "", scheduling.RoleResponse)
	openers := songIDSet(openerRoles)
	responses := songIDSet(responseRoles)

	// Best-effort, same as titles/roles above: tags are a search/display
	// nicety on the candidate list, so a lookup failure just means no tag
	// badges/search text rather than failing the whole page.
	allTags, _ := sc.store.ListSongTags(ctx, organizationID, "", "")
	tagCategories, _ := sc.store.ListSongTagCategories(ctx)
	tagsBySong := songTagsBySong(allTags)
	tagCategoryLabels := songTagCategoryLabelMap(tagCategories)

	// AllSongs/TagCategories/RelationshipTypes/RoleTypes back the "Quick
	// add" panel's <select> pickers (see views.SetBuilderData) - best-effort
	// like the lookups above, since a failure here only costs that one
	// panel's dropdowns, not the page.
	data.AllSongs = songs
	data.TagCategories = tagCategories
	data.RelationshipTypes, _ = sc.store.ListSongRelationshipTypes(ctx)
	data.RoleTypes, _ = sc.store.ListSongRoleTypes(ctx)

	inSet := make(map[string]bool, len(setSongs))
	for _, ss := range setSongs {
		inSet[ss.SongID] = true
		if ss.PCOItemID == "" {
			data.UnpublishedCount++
		}
		if ss.Moved {
			data.ReorderPending = true
		}
		data.SetSongs = append(data.SetSongs, views.SetSongView{
			ID:           ss.ID,
			SongID:       ss.SongID,
			Title:        titleOrID(titles, ss.SongID),
			Position:     ss.Position,
			IsOpener:     openers[ss.SongID],
			IsResponse:   responses[ss.SongID],
			Published:    ss.PCOItemID != "",
			AnchorItemID: ss.AnchorItemID,
			Moved:        ss.Moved,
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
		data.ServiceOrder = renderServiceOrder(merged, titles, openers, responses)

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

		var roles []string
		if openers[song.ID] {
			roles = append(roles, scheduling.RoleOpener)
		}
		if responses[song.ID] {
			roles = append(roles, scheduling.RoleResponse)
		}

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

		data.Candidates = append(data.Candidates, views.CandidateSongView{
			SongID:           song.ID,
			Title:            song.Attributes.Title,
			IsOpener:         openers[song.ID],
			IsResponse:       responses[song.ID],
			ConflictTitles:   conflictTitles,
			RecentlyRepeated: repeated[song.ID],
			RolesAttr:        strings.Join(roles, " "),
			Tags:             tagViews,
			TagsAttr:         strings.Join(tagValues, " "),
		})
	}

	return data
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

// renderServiceOrder turns a merge into display rows plus the anchor
// picker's option list (every foreign item, in order, prefixed by "Top of
// service").
func renderServiceOrder(rows []mergedRow, titles map[string]string, openers, responses map[string]bool) []views.ServiceOrderRow {
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
		out = append(out, views.ServiceOrderRow{
			IsSong: true,
			Song: views.SetSongView{
				ID:           ss.ID,
				SongID:       ss.SongID,
				Title:        titleOrID(titles, ss.SongID),
				Position:     ss.Position,
				IsOpener:     openers[ss.SongID],
				IsResponse:   responses[ss.SongID],
				Published:    ss.PCOItemID != "",
				AnchorItemID: ss.AnchorItemID,
				Moved:        ss.Moved,
			},
		})
	}

	return out
}

func songIDSet(roles []scheduling.SongRole) map[string]bool {
	set := make(map[string]bool, len(roles))
	for _, r := range roles {
		set[r.SongID] = true
	}
	return set
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
