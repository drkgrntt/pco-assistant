package controllers

// AddPlanItem/RepositionPlanItem: adding a plain, non-song item (a sermon
// slot, a video, a section header, ...) to a plan from the Build page
// itself, and moving one once it's there - instead of switching over to
// Planning Center for either. See addPlanItemRows, the Order of service
// table's trailing "+ Add item to plan" row (quick_action.templ), and that
// same table's drag-handle on every header/foreign row (set_builder.templ).

import (
	"sort"

	"pco-assistant/internal/logger"
	"pco-assistant/internal/server/middleware"
	views "pco-assistant/internal/views/scheduling"

	pco "github.com/drkgrntt/go-pco"
	"github.com/gofiber/fiber/v2"
)

// AddPlanItem creates a real Planning Center item directly in the live
// plan - unlike a set song, a foreign item has no local draft concept
// anywhere in this app (mergeServiceOrder's own doc comment, and CLAUDE.md's
// Sync model section: non-song items are read-only context this tool
// doesn't manage), so there's no Publish step to defer to. This pushes
// straight to PCO the moment it's submitted, same as ArchiveSong's direct
// pco.UpdateSong write just above it in the set builder.
//
// Always appended after the plan's current last item, never inserted at a
// chosen position - this app's own order-of-service table only supports
// drag-to-reorder for this set's own songs (set_builder.templ's
// drag-handle), so moving a newly-added item earlier is still a Planning
// Center action, the same as it is for any other foreign item today.
// Confirmed live: leaving CreateItemParams.Sequence at its Go zero value
// (0) doesn't put the item first - Planning Center auto-appends it after
// whatever the plan's real, current last item is (go-pco's CreateItem
// always sends "sequence": 0 explicitly, not an omitted field, and PCO
// treats that as "no sequence given," not "position 0" either way). So
// this needs no pre-create GetItems fetch to compute a next sequence
// number itself, unlike Publish's own item creates (sets_publish.go),
// which do fetch existing items first - those need the fetch anyway for
// the merge/reorder that follows a whole-set publish, so it isn't a spare
// call there the way a second one would be here for a single add.
//
// Only ever creates ItemTypeItem or ItemTypeHeader - confirmed live
// against the real API: a create with ItemType "media" comes back silently
// downgraded to a plain "item" (no attached media, so there's nothing for
// Planning Center to actually treat as media), and a header item's own
// Length always comes back 0 regardless of what's sent, even though the
// create itself succeeds - Planning Center just doesn't track a duration
// for a section divider. addPlanItemRows' own item-type select only
// offers these two real options, and hides/disables the length fields for
// Section header client-side (toggleAddPlanItemLengthFields, scheduling.js)
// so the form doesn't imply a header's length is going anywhere; anything
// else this switch doesn't recognize also falls back to a plain item.
func (sc *SchedulingController) AddPlanItem(c *fiber.Ctx) error {
	organizationID := middleware.CurrentOrganizationID(c)
	ctx := middleware.RequestContext(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")

	var body struct {
		Title         string `form:"title"`
		Description   string `form:"description"`
		ItemType      string `form:"item_type"`
		LengthMinutes int    `form:"length_minutes"`
		LengthSeconds int    `form:"length_seconds"`
	}

	errMsg := ""
	if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else if body.Title == "" {
		errMsg = "Title is required"
	} else {
		itemType := body.ItemType
		switch itemType {
		case pco.ItemTypeHeader, pco.ItemTypeItem:
			// A real, supported item type - keep it as-is.
		default:
			itemType = pco.ItemTypeItem
		}

		if _, err := pco.CreateItem(ctx, serviceTypeID, planID, &pco.CreateItemParams{
			Title:       body.Title,
			Description: body.Description,
			ItemType:    itemType,
			Length:      body.LengthMinutes*60 + body.LengthSeconds,
		}); err != nil {
			logger.ErrorContext(ctx, "create plan item", "error", err)
			errMsg = "Couldn't add that item in Planning Center: " + friendlyPCOErr(err)
		}
	}

	return render(c, views.SetBuilderBody(sc.buildSetBuilderDataWithErr(ctx, organizationID, serviceTypeID, planID, errMsg)))
}

// RepositionPlanItem is what the Order of service table's drag-and-drop
// calls when the dragged row is a header or plain item (a set song's own
// row calls RepositionSetSong instead, sets_reorder.go, which only ever
// touches this app's local draft - never PCO directly). A foreign item has
// no local draft to move within, so this moves the real Planning Center
// item directly and immediately, the same "no draft to defer to" reasoning
// as AddPlanItem's own create.
//
// ?after= is the exact same drag-and-drop token RepositionSetSong reads
// ("item:<pco_item_id>" or "row:<local set-song id>" - see
// parseAfterToken), resolved here to the nearest row - walking backward
// from the drop point - that's an actual Planning Center item right now
// (anchorRealItemID): a not-yet-published set song isn't part of Planning
// Center's own order at all, so dropping a foreign item next to one of
// those resolves to whatever real item precedes it instead. pco.
// ReorderItems always wants literally every item in the plan, in order
// (the same contract PublishSet's own finalOrder build already relies on,
// sets_publish.go) - buildReorderedItemIDs starts from this plan's real,
// current item list and only moves draggedItemID within it, so every
// other item's position is preserved exactly.
func (sc *SchedulingController) RepositionPlanItem(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	ctx := middleware.RequestContext(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")
	draggedItemID := c.Params("item_id")

	var body struct {
		After string `form:"after"`
	}

	errMsg := ""
	if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else if set, err := sc.store.GetOrCreateSet(ctx, orgID, serviceTypeID, planID); err != nil {
		errMsg = err.Error()
	} else if setSongs, err := sc.store.ListSetSongs(ctx, set.ID); err != nil {
		errMsg = err.Error()
	} else if pcoItems, err := pco.GetItems(ctx, serviceTypeID, planID, nil); err != nil {
		errMsg = "loading the current plan order from Planning Center: " + friendlyPCOErr(err)
	} else {
		sort.Slice(pcoItems.Data, func(i, j int) bool {
			return pcoItems.Data[i].Attributes.Sequence < pcoItems.Data[j].Attributes.Sequence
		})

		merged := mergeServiceOrder(pcoItems.Data, setSongs)
		anchorItemID := anchorRealItemID(merged, body.After)
		newOrder := buildReorderedItemIDs(pcoItems.Data, draggedItemID, anchorItemID)

		// Skip the call entirely when nothing would actually move - same
		// "avoid a pointless write" convention syncLeaderNote/InfoPending
		// follow elsewhere in this package, and the common case for a
		// drag that resolves back to its own current position (the
		// client already no-ops that case itself, but a second guard here
		// costs nothing and covers a stale/replayed request too).
		if !itemOrderEqual(pcoItems.Data, newOrder) {
			if err := pco.ReorderItems(ctx, serviceTypeID, planID, newOrder); err != nil {
				logger.ErrorContext(ctx, "reposition plan item", "error", err, "item_id", draggedItemID)
				errMsg = "Couldn't reorder that item in Planning Center: " + friendlyPCOErr(err)
			}
		}
	}

	return render(c, views.SetBuilderBody(sc.buildSetBuilderDataWithErr(ctx, orgID, serviceTypeID, planID, errMsg)))
}

// UpdatePlanItem edits a real Planning Center item's title, description,
// and length directly and immediately - same "no local draft to defer to"
// reasoning as AddPlanItem. Item type is never editable here: go-pco's
// UpdateItemParams has no ItemType field at all (PCO rejects a Sequence
// PATCH too, per that struct's own doc comment - reordering goes through
// ReorderItems/RepositionPlanItem instead, not a PATCH), so an item
// created as a header can't become a plain item afterward, or back - only
// deleting and recreating it can, same as it always could.
//
// LengthMinutes/LengthSeconds always come pre-filled with this item's
// current length (addPlanItemRows' edit form, quick_action.templ) rather
// than starting blank the way AddPlanItem's create form does - pco.
// UpdateItem's Length is a plain int, sent on every call, not an optional
// "leave this alone" the way its Title/Description pointers are used here;
// leaving the fields blank on an edit that's only about the title would
// otherwise silently zero out a real length.
func (sc *SchedulingController) UpdatePlanItem(c *fiber.Ctx) error {
	organizationID := middleware.CurrentOrganizationID(c)
	ctx := middleware.RequestContext(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")
	itemID := c.Params("item_id")

	var body struct {
		Title         string `form:"title"`
		Description   string `form:"description"`
		LengthMinutes int    `form:"length_minutes"`
		LengthSeconds int    `form:"length_seconds"`
	}

	errMsg := ""
	if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else if body.Title == "" {
		errMsg = "Title is required"
	} else {
		length := body.LengthMinutes*60 + body.LengthSeconds
		if _, err := pco.UpdateItem(ctx, serviceTypeID, planID, itemID, &pco.UpdateItemParams{
			Title:       &body.Title,
			Description: &body.Description,
			Length:      &length,
		}); err != nil {
			logger.ErrorContext(ctx, "update plan item", "error", err, "item_id", itemID)
			errMsg = "Couldn't update that item in Planning Center: " + friendlyPCOErr(err)
		}
	}

	return render(c, views.SetBuilderBody(sc.buildSetBuilderDataWithErr(ctx, organizationID, serviceTypeID, planID, errMsg)))
}

// RemovePlanItem deletes a real Planning Center item directly and
// immediately - same "no local draft to defer to" reasoning as
// AddPlanItem/UpdatePlanItem. Unlike RemoveSetSong (sets_detail.go), which
// only ever removes this app's own local draft row and leaves anything
// PCO-side to the next Publish/Discard, there's no local row here at all -
// this is the only removal path a foreign item has.
func (sc *SchedulingController) RemovePlanItem(c *fiber.Ctx) error {
	organizationID := middleware.CurrentOrganizationID(c)
	ctx := middleware.RequestContext(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")
	itemID := c.Params("item_id")

	errMsg := ""
	if err := pco.DeleteItem(ctx, serviceTypeID, planID, itemID); err != nil {
		logger.ErrorContext(ctx, "remove plan item", "error", err, "item_id", itemID)
		errMsg = "Couldn't remove that item from Planning Center: " + friendlyPCOErr(err)
	}

	return render(c, views.SetBuilderBody(sc.buildSetBuilderDataWithErr(ctx, organizationID, serviceTypeID, planID, errMsg)))
}

// anchorRealItemID resolves a drag-and-drop "after" token to the id of the
// nearest row - starting at the token's own row and walking backward
// through the merged order - that's an actual Planning Center item right
// now: an "item:" token already names one directly; a "row:" token (a set
// song) resolves to that song's own PCOItemID if it's been published, or
// keeps walking backward past any not-yet-published song (which isn't
// part of Planning Center's own order at all) until it finds a row that
// is one - a foreign item, or another already-published song. "" means
// the top of the real order.
func anchorRealItemID(merged []mergedRow, afterToken string) string {
	directItemID, afterRowID, err := parseAfterToken(afterToken)
	if err != nil || directItemID != "" {
		return directItemID
	}
	if afterRowID == 0 {
		return ""
	}

	for i, row := range merged {
		if row.Song == nil || row.Song.ID != afterRowID {
			continue
		}
		for j := i; j >= 0; j-- {
			r := merged[j]
			if r.Item != nil {
				return r.Item.ID
			}
			if r.Song != nil && r.Song.PCOItemID != "" {
				return r.Song.PCOItemID
			}
		}
		break
	}
	return ""
}

// buildReorderedItemIDs returns pcoItems' own ids (already sorted by
// Sequence) with draggedItemID moved to immediately follow anchorItemID -
// "" anchors at the very top. Falls back to appending draggedItemID at the
// end - same tolerance mergeServiceOrder's own doc comment describes for a
// stale anchor - if anchorItemID isn't actually found (a concurrent PCO
// edit removed it, say), rather than silently dropping the item from the
// reorder call entirely.
func buildReorderedItemIDs(pcoItems []pco.ItemData, draggedItemID, anchorItemID string) []string {
	ids := make([]string, 0, len(pcoItems))
	for _, item := range pcoItems {
		if item.ID != draggedItemID {
			ids = append(ids, item.ID)
		}
	}
	if anchorItemID == draggedItemID {
		anchorItemID = ""
	}
	if anchorItemID == "" {
		return append([]string{draggedItemID}, ids...)
	}

	out := make([]string, 0, len(ids)+1)
	placed := false
	for _, id := range ids {
		out = append(out, id)
		if id == anchorItemID {
			out = append(out, draggedItemID)
			placed = true
		}
	}
	if !placed {
		out = append(out, draggedItemID)
	}
	return out
}

// itemOrderEqual reports whether newOrder is exactly pcoItems' current
// sequence order, id for id - true means the reorder call below would be a
// no-op, not worth spending on Planning Center's rate budget.
func itemOrderEqual(pcoItems []pco.ItemData, newOrder []string) bool {
	if len(pcoItems) != len(newOrder) {
		return false
	}
	for i, item := range pcoItems {
		if item.ID != newOrder[i] {
			return false
		}
	}
	return true
}
