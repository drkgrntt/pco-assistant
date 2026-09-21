package controllers

// Dismiss/Re-enable actions for the Songs tab's dismissable warnings
// (docs/plans/shipped/dismissable-warnings-plan.md) - avoid-pairing,
// recently-repeated, and vocal-range-mismatch. Both handlers take
// warning_kind/subject_key straight from the submitting form body (see
// dismissableWarningMenu, set_builder.templ, for the exact hidden inputs)
// rather than re-deriving them server-side - the template already computed
// the correct key for whichever badge instance was clicked (see
// avoidPairingSubjectKey/recentRepeatSubjectKey/vocalRangeMismatchSubjectKey,
// sets_builder.go), so there's nothing to re-derive here. Same
// full-body-re-render convention as every other Songs-tab mutation in this
// file (DuplicateSetSong, sets_detail.go) - re-rendering just the one badge
// would need a much narrower OOB target than this app's htmx conventions
// currently bother with for the set builder.

import (
	"pco-assistant/internal/server/middleware"
	views "pco-assistant/internal/views/scheduling"

	"github.com/gofiber/fiber/v2"
)

// DismissBuildWarning records a warning as dismissed - see
// scheduling.Store.DismissWarning. Idempotent: dismissing an
// already-dismissed warning just refreshes who dismissed it and when,
// rather than erroring.
func (sc *SchedulingController) DismissBuildWarning(c *fiber.Ctx) error {
	organizationID := middleware.CurrentOrganizationID(c)
	ctx := middleware.RequestContext(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")
	personID, _ := middleware.CurrentPerson(c)
	warningKind := c.FormValue("warning_kind")
	subjectKey := c.FormValue("subject_key")

	errMsg := ""
	if warningKind == "" || subjectKey == "" {
		errMsg = "warning_kind and subject_key are required"
	} else if _, err := sc.store.DismissWarning(ctx, organizationID, personID, warningKind, subjectKey); err != nil {
		errMsg = "Couldn't dismiss that warning: " + err.Error()
	}

	return render(c, views.SetBuilderBody(sc.buildSetBuilderDataWithErr(ctx, organizationID, serviceTypeID, planID, errMsg)))
}

// UndismissBuildWarning removes a dismissal - "Re-enable warning" on an
// already-dismissed badge. A no-op (not an error) when nothing was
// actually dismissed, same tolerance as scheduling.Store.UndismissWarning
// itself.
func (sc *SchedulingController) UndismissBuildWarning(c *fiber.Ctx) error {
	organizationID := middleware.CurrentOrganizationID(c)
	ctx := middleware.RequestContext(c)
	serviceTypeID := c.Params("service_type_id")
	planID := c.Params("plan_id")
	warningKind := c.FormValue("warning_kind")
	subjectKey := c.FormValue("subject_key")

	errMsg := ""
	if warningKind == "" || subjectKey == "" {
		errMsg = "warning_kind and subject_key are required"
	} else if err := sc.store.UndismissWarning(ctx, organizationID, warningKind, subjectKey); err != nil {
		errMsg = "Couldn't re-enable that warning: " + err.Error()
	}

	return render(c, views.SetBuilderBody(sc.buildSetBuilderDataWithErr(ctx, organizationID, serviceTypeID, planID, errMsg)))
}
