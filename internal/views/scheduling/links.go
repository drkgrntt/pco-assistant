package scheduling

// pcoPlanURL builds the Planning Center Services web app URL for a plan -
// backs the builder page's "Open in Planning Center" deep link, so jumping
// from a draft here to the real, live plan (to see it exactly as
// volunteers/PCO's own UI will) is one click instead of hunting through
// PCO's own navigation. Confirmed against PCO's actual routing (the
// /plans/:id shape, unauthenticated requests round-trip
// through PCO's login redirect with this exact path preserved in `return`).
func pcoPlanURL(planID string) string {
	return "https://services.planningcenteronline.com/" + "/plans/" + planID
}
