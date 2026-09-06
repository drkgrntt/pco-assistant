package controllers

// RefreshRosterTeamData is the roster builder's manual "Refresh team data"
// action - the remedy for fetchRosterSharedPCOData's cache staleness
// window (see rosters_shared_cache.go's own doc comment for the full
// reasoning: 20s TTL, no invalidation on local writes since nothing this
// app does locally ever changes what Planning Center returns for team
// members/positions). Bypasses the cache outright and rebuilds the whole
// roster, not just one position group - a changed team member list or
// position count can affect every group on the page, not just one, unlike
// AddRosterAssignment/RemoveRosterAssignment's narrower single-group
// scope. Mirrors RevertRoster's own full-rebuild-and-render shape
// (rosters_publish.go), just without touching any local roster data.
import (
	"pco-assistant/internal/server/middleware"
	views "pco-assistant/internal/views/scheduling"

	"github.com/gofiber/fiber/v2"
)

func (sc *SchedulingController) RefreshRosterTeamData(c *fiber.Ctx) error {
	ctx := middleware.RequestContext(c)
	orgID := middleware.CurrentOrganizationID(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")

	invalidateRosterSharedPCOCache(serviceTypeID, planID)

	data := sc.buildRosterBuilderData(ctx, orgID, serviceTypeID, planID)
	data.PublishMsg = "Refreshed team data from Planning Center."

	return render(c, views.RosterBuilderBody(data))
}
