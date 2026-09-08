package views

// HomeData is the signed-in home page's view model - see
// HomeController.buildHomeData. Every field here is best-effort: a
// failed lookup just leaves that section empty/zero rather than
// breaking the page, same tolerance as every other page-builder in this
// codebase.
type HomeData struct {
	// UpcomingPlans is at most one row per service type (its next plan, if
	// any), optionally followed immediately by a second row (see
	// HomePlanRow.IsNextUnplanned) when that next plan is already fully
	// planned and a later plan within HomeController's
	// homeLookAheadPlansCap still needs work - a "needs attention" list,
	// not a full picker (that's what Build a Service is for).
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
	// Onboarding is the promise-grouped setup checklist (Phase 3 of
	// docs/plans/in-progress/onboarding-and-ambient-framing-plan.md) that
	// replaced both the old flat Setup link list and the dismissible
	// nudge card that briefly preceded it - see buildHomeData. Always
	// populated: it's the permanent home for setup now, not a transient
	// prompt, so there's nothing to dismiss or replay any more.
	Onboarding OnboardingChecklist
}

// OnboardingChecklist is the home page's setup checklist - two
// promise-grouped sections (see OnboardingGroup) plus the summary counts
// rendered next to the section heading.
type OnboardingChecklist struct {
	Groups []OnboardingGroup
	// Done/Trackable count only items whose completion state is actually
	// knowable locally (see OnboardingItem.Trackable's doc comment) -
	// Position Preferences is the one exception, excluded from both
	// counts rather than counted as permanently "not done".
	Done, Trackable int
}

// OnboardingGroup is one promise-grouped section of the checklist -
// "Protect the service" or "Protect the team" - mirroring
// docs/ideas.md's Vision/positioning split, each with a one-line why and
// its item list.
type OnboardingGroup struct {
	Title string
	Intro string
	Items []OnboardingItem
}

// OnboardingItem is one setup link in the checklist.
type OnboardingItem struct {
	Label       string
	Description string
	Href        string
	// Done reflects real local data via a cheap existence check
	// (HasAnySongRelationships and its siblings in internal/scheduling) -
	// only meaningful when Trackable is true.
	Done bool
	// Trackable is false only for Position Preferences: that data lives
	// entirely in Planning Center (schedule_preference writes straight
	// through - see CLAUDE.md's Sync model section), and checking it live
	// across every team/position on every home page load would mean a
	// real PCO call on a hot render path, which docs/pco-api-risks.md's
	// caution against that rules out. The template renders a plain link
	// with no done/not-done badge for this one item rather than a
	// permanently-false, misleading one.
	Trackable bool
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
	// IsNextUnplanned marks a second, look-ahead row (see
	// HomeController.homeLookAheadPlansCap) rendered directly beneath its
	// service type's primary "next plan" row - true only when the primary
	// row is already fully planned and this later plan still needs work.
	// home.templ uses it to style this row as a distinct follow-up rather
	// than a second, unrelated "next plan".
	IsNextUnplanned bool
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
