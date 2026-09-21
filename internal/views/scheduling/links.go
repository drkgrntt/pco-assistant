package scheduling

import "fmt"

// adjacentPlanURL builds the link for BuildPlan's Previous/Next plan
// buttons - the same builder page, just pointed at PCO's own
// previous_plan/next_plan relationship id for this service type (see
// SetBuilderData.PrevPlanID/NextPlanID). Carries activeTab across so
// paging through plans from the Team tab doesn't bounce back to Songs.
func adjacentPlanURL(serviceTypeID, planID, activeTab string) string {
	url := fmt.Sprintf("/scheduling/build/%s/%s", serviceTypeID, planID)
	if activeTab == "team" {
		url += "?tab=team"
	}
	return url
}

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

// pcoSongURL builds the Planning Center Services web app URL for a song -
// backs the song detail page's own "Open in Planning Center" deep link
// (song_detail.templ), the song-library counterpart of pcoPlanURL above -
// same reasoning: jumping to the real, live song record is one click
// instead of hunting through Planning Center's own navigation.
func pcoSongURL(songID string) string {
	return "https://services.planningcenteronline.com/songs/" + songID
}
