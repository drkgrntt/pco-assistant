package controllers

import (
	"pco-assistant/internal/server/middleware"
	views "pco-assistant/internal/views/scheduling"

	"github.com/gofiber/fiber/v2"
)

// RunSheetPage renders the printable "day-of" run sheet for one plan - the
// combined service/run sheet from ideas.md's "Run sheet (day-of output)"
// section: set list, key/tempo/meter/length, item notes, role badges, and
// the roster, flattened onto one page a director can print (or "print to
// PDF" via the browser) and carry into rehearsal or hand to the sound
// booth.
//
// Deliberately a pure view over data BuildPlanPage already computes: same
// buildSetBuilderData/buildRosterBuilderData calls, same ctx/org/plan
// resolution, no additional Planning Center requests and no new storage.
// ideas.md's original wording also mentions "capo" and "gear needs," but
// neither concept exists anywhere in this codebase's data model (confirmed
// by search) - this only surfaces what's actually there.
func (sc *SchedulingController) RunSheetPage(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)

	ctx := middleware.RequestContext(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")

	setData := sc.buildSetBuilderData(ctx, orgID, serviceTypeID, planID)
	rosterData := sc.buildRosterBuilderData(ctx, orgID, serviceTypeID, planID)

	return render(c, views.RunSheet(setData, rosterData))
}
