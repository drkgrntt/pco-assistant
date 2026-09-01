package controllers

import (
	"testing"

	pco "github.com/drkgrntt/go-pco"
)

func plan(title, dates string) pco.PlanData {
	return pco.PlanData{Attributes: pco.PlanAttributes{Title: title, Dates: dates}}
}

func TestFilterUpcomingPlansMatchesTitleOrDatesCaseInsensitive(t *testing.T) {
	plans := []pco.PlanData{
		plan("Easter Sunday", "April 5, 2026"),
		plan("", "April 12, 2026"),
		plan("Baptism Service", "May 3, 2026"),
	}

	got := filterUpcomingPlans(plans, "easter", nil)
	if len(got) != 1 || got[0].Attributes.Title != "Easter Sunday" {
		t.Fatalf("expected title match to find Easter Sunday, got %+v", got)
	}

	// A plan with no custom title only has its Dates string visible in the
	// list, so search has to reach it too.
	got = filterUpcomingPlans(plans, "april 12", nil)
	if len(got) != 1 || got[0].Attributes.Dates != "April 12, 2026" {
		t.Fatalf("expected dates match to find the untitled April 12 plan, got %+v", got)
	}

	got = filterUpcomingPlans(plans, "", nil)
	if len(got) != 3 {
		t.Fatalf("expected blank search to match everything, got %d", len(got))
	}

	got = filterUpcomingPlans(plans, "nonexistent", nil)
	if len(got) != 0 {
		t.Fatalf("expected no matches, got %+v", got)
	}
}

// TestFilterUpcomingPlansMatchesNoteText confirms the picker's search also
// reaches each plan's local at-a-glance note (plan_notes/GetPlanNote) - the
// whole point of the note existing at all ("the one with the guest
// speaker") - even when the search term appears nowhere in the plan's own
// title or dates.
func TestFilterUpcomingPlansMatchesNoteText(t *testing.T) {
	guestSpeaker := plan("", "June 7, 2026")
	guestSpeaker.ID = "1"
	regular := plan("", "June 14, 2026")
	regular.ID = "2"
	plans := []pco.PlanData{guestSpeaker, regular}
	notes := map[string]string{"1": "Guest speaker - Pastor Lee"}

	got := filterUpcomingPlans(plans, "guest speaker", notes)
	if len(got) != 1 || got[0].ID != "1" {
		t.Fatalf("expected note match to find plan 1, got %+v", got)
	}

	// A nil/empty notes map (no notes saved, or the lookup failed) just
	// means nothing extra matches - same as before notes existed.
	got = filterUpcomingPlans(plans, "guest speaker", nil)
	if len(got) != 0 {
		t.Fatalf("expected no matches with a nil notes map, got %+v", got)
	}
}

func TestPaginateUpcomingPlansSlicesAndReportsTotalPages(t *testing.T) {
	plans := make([]pco.PlanData, 25)
	for i := range plans {
		plans[i] = plan("", "")
	}

	page1, currentPage, totalPages := paginateUpcomingPlans(plans, 1)
	if len(page1) != buildPlansPageSize || currentPage != 1 || totalPages != 3 {
		t.Fatalf("page 1: got %d rows, page %d of %d", len(page1), currentPage, totalPages)
	}

	page3, currentPage, totalPages := paginateUpcomingPlans(plans, 3)
	if len(page3) != 5 || currentPage != 3 || totalPages != 3 {
		t.Fatalf("page 3 (partial): got %d rows, page %d of %d", len(page3), currentPage, totalPages)
	}
}

// TestPaginateUpcomingPlansClampsOutOfRangePage confirms requesting a page
// past the end (e.g. a new search narrowed the results out from under a
// pagination link built against the old, larger total) falls back to the
// last real page instead of an empty slice.
func TestPaginateUpcomingPlansClampsOutOfRangePage(t *testing.T) {
	plans := []pco.PlanData{plan("Only plan", "")}

	page, currentPage, totalPages := paginateUpcomingPlans(plans, 99)
	if len(page) != 1 || currentPage != 1 || totalPages != 1 {
		t.Fatalf("expected clamp to page 1 of 1 with the one plan, got %d rows, page %d of %d", len(page), currentPage, totalPages)
	}
}

func TestPaginateUpcomingPlansEmptyStillReportsOnePage(t *testing.T) {
	page, currentPage, totalPages := paginateUpcomingPlans(nil, 1)
	if len(page) != 0 || currentPage != 1 || totalPages != 1 {
		t.Fatalf("expected 0 rows, page 1 of 1, got %d rows, page %d of %d", len(page), currentPage, totalPages)
	}
}
