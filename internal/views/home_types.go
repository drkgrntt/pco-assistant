package views

// HomeData is the signed-in home page's view model - see
// HomeController.buildHomeData. Every field here is best-effort: a
// failed lookup just leaves that section empty/zero rather than
// breaking the page, same tolerance as every other page-builder in this
// codebase.
type HomeData struct {
	// UpcomingPlans is at most one row per service type (its next plan,
	// if any) - a "needs attention" list, not a full picker (that's what
	// Build a Service is for).
	UpcomingPlans []HomePlanRow
	// SuggestionCount is how many Song Tag Suggestions groups are
	// waiting for review (see the controller's songTagSuggestions) - 0
	// hides the nudge entirely.
	SuggestionCount int
	// OrgStatus/OrgPlan/TrialDaysLeft mirror BillingData's own trial
	// fields - shown here too since a trial ending is worth surfacing
	// somewhere more visible than a page you only visit on purpose.
	OrgStatus     string
	OrgPlan       string
	TrialDaysLeft int
}

// HomePlanRow is one upcoming plan on the home page - the same
// SetStatus/RosterStatus values (and PlanStatusBadge rendering) as
// views/scheduling.PlanRow, plus which service type it belongs to, since
// (unlike the Build page's picker) this list spans every service type at
// once.
type HomePlanRow struct {
	ServiceTypeID, PlanID   string
	ServiceTypeName         string
	Title, Dates            string
	SetStatus, RosterStatus string
}

// TargetURL is where this row's link actually goes - straight into
// whichever tab still needs work, now that Build a Service's Songs/Team
// tabs are directly linkable (?tab=, see BuildPlanPage). Defaults to
// Songs (no query param needed, since that's already the tab's own
// default) except when Songs is already fully "planned" and Team isn't -
// otherwise this list would always land someone back on a tab with
// nothing left to do, on a plan that's only still here because its *other*
// tab needs attention.
func (p HomePlanRow) TargetURL() string {
	url := "/scheduling/build/" + p.ServiceTypeID + "/" + p.PlanID
	if p.SetStatus == "planned" && p.RosterStatus != "planned" {
		url += "?tab=team"
	}
	return url
}
