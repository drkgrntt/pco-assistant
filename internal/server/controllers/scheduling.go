package controllers

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"pco-assistant/internal/scheduling"
	"pco-assistant/internal/server/middleware"
	views "pco-assistant/internal/views/scheduling"

	pco "github.com/drkgrntt/go-pco"
	"github.com/gofiber/fiber/v2"
)

// pcoSongPickerPageSize is PCO's max per_page, and pcoSongPickerMaxPages
// caps how many pages fetchAllSongs will walk - a safety valve so an
// unexpectedly huge library can't turn one page load into an unbounded
// number of PCO requests.
const (
	pcoSongPickerPageSize = 100
	pcoSongPickerMaxPages = 20
)

// personSearchResultLimit caps how many matches the person picker's
// search-as-you-type shows at once. Unlike songs, a People directory is
// often far too large to fetch in full, so people are looked up on demand
// by search query instead of listed up front.
const personSearchResultLimit = 8

// fetchAllSongs pages through the PCO song library, ordered by title, for
// use as a picker's option list (song relationships, song/person
// preferences). It returns whatever it managed to fetch even on a
// mid-pagination error, since a stale-but-nonempty picker beats an empty one.
func fetchAllSongs(ctx context.Context) ([]pco.SongData, error) {
	var songs []pco.SongData

	for page := 0; page < pcoSongPickerMaxPages; page++ {
		resp, err := pco.GetSongs(ctx, &pco.SongsParams{
			OrderBy: "title",
			PerPage: pcoSongPickerPageSize,
			Offset:  page * pcoSongPickerPageSize,
		})
		if err != nil {
			return songs, err
		}

		songs = append(songs, resp.Data...)

		if len(resp.Data) < pcoSongPickerPageSize || len(songs) >= resp.Meta.TotalCount {
			break
		}
	}

	return songs, nil
}

// songTitleMap builds a song ID -> title lookup from a picker's song list,
// for resolving a relationship's stored song IDs back to display titles.
func songTitleMap(songs []pco.SongData) map[string]string {
	titles := make(map[string]string, len(songs))
	for _, song := range songs {
		titles[song.ID] = song.Attributes.Title
	}
	return titles
}

// songRelationshipTypeLabelMap builds a relationship_type code -> label
// lookup from ListSongRelationshipTypes, for resolving a relationship's
// stored type code back to its display label (see RelationshipsTable).
func songRelationshipTypeLabelMap(types []scheduling.SongRelationshipType) map[string]string {
	labels := make(map[string]string, len(types))
	for _, t := range types {
		labels[t.Code] = t.Label
	}
	return labels
}

// songPreferenceTypeLabelMap is songRelationshipTypeLabelMap's counterpart
// for ListSongPreferenceTypes (see PreferencesTable).
func songPreferenceTypeLabelMap(types []scheduling.SongPreferenceType) map[string]string {
	labels := make(map[string]string, len(types))
	for _, t := range types {
		labels[t.Code] = t.Label
	}
	return labels
}

// songTagCategoryLabelMap is songRelationshipTypeLabelMap's counterpart for
// ListSongTagCategories (see SongTagsTable and the set builder's candidate
// tag badges).
func songTagCategoryLabelMap(categories []scheduling.SongTagCategory) map[string]string {
	labels := make(map[string]string, len(categories))
	for _, cat := range categories {
		labels[cat.Code] = cat.Label
	}
	return labels
}

// songTagsBySong groups a flat tag list by song id, for annotating either
// one setup-page table row or the whole set-builder candidate pool at once
// without a query per song.
func songTagsBySong(tags []scheduling.SongTag) map[string][]scheduling.SongTag {
	bySong := make(map[string][]scheduling.SongTag, len(tags))
	for _, tag := range tags {
		bySong[tag.SongID] = append(bySong[tag.SongID], tag)
	}
	return bySong
}

// personNameMap resolves each unique person ID referenced by preferences to
// its PCO display name, for showing names instead of raw IDs in the
// preferences table. Unlike songTitleMap, this doesn't come from a bulk
// listing - a People directory can be far bigger than a song library - so
// it looks up only the handful of IDs actually in use, one GetPerson call
// each. An ID that fails to resolve (e.g. the person was since deleted) is
// simply left out of the map; callers fall back to showing the raw ID.
func personNameMap(ctx context.Context, preferences []scheduling.SongPersonPreference) map[string]string {
	names := make(map[string]string, len(preferences))
	for _, pref := range preferences {
		if _, ok := names[pref.PersonID]; ok {
			continue
		}

		if person, err := pco.GetPerson(ctx, pref.PersonID); err == nil {
			names[pref.PersonID] = person.Data.Attributes.Name
		}
	}
	return names
}

// parseFormID reads the "id" field a setup page's create/edit form carries
// when editing an existing row (see editRow in web/static/scheduling.js,
// populated from a table row's Edit button) - present=false means "no id",
// i.e. this submit is a create, not an edit. A non-nil err means the field
// was there but wasn't a valid integer, which the caller should treat as
// bad input rather than silently falling back to create.
func parseFormID(c *fiber.Ctx) (id int64, present bool, err error) {
	raw := strings.TrimSpace(c.FormValue("id"))
	if raw == "" {
		return 0, false, nil
	}
	id, err = strconv.ParseInt(raw, 10, 64)
	return id, true, err
}

// SchedulingController serves the htmx-driven HTML pages for scheduling
// data, calling the same *scheduling.Store the JSON API
// (SchedulingAPIController, mounted at /api/scheduling) uses - these are
// two different response formats over the same data, not two different
// data layers.
type SchedulingController struct {
	store *scheduling.Store
}

func NewSchedulingController(store *scheduling.Store) *SchedulingController {
	return &SchedulingController{store: store}
}

func (sc *SchedulingController) RegisterRoutes(router fiber.Router) {
	group := router.Group("/scheduling")

	group.Get("/song-relationships", sc.RelationshipsPage)
	group.Post("/song-relationships", sc.CreateRelationship)
	group.Delete("/song-relationships/:id", sc.DeleteRelationship)

	group.Get("/song-preferences", sc.PreferencesPage)
	group.Post("/song-preferences", sc.SetPreference)
	group.Delete("/song-preferences/:id", sc.DeletePreference)
	group.Get("/people-search", sc.PeopleSearch)

	group.Get("/song-roles", sc.SongRolesPage)
	group.Post("/song-roles", sc.CreateSongRole)
	group.Delete("/song-roles/:id", sc.DeleteSongRole)

	group.Get("/song-tags", sc.SongTagsPage)
	group.Post("/song-tags", sc.CreateSongTag)
	group.Delete("/song-tags/:id", sc.DeleteSongTag)

	group.Get("/person-relationships", sc.PersonRelationshipsPage)
	group.Post("/person-relationships", sc.CreatePersonRelationship)
	group.Delete("/person-relationships/:id", sc.DeletePersonRelationship)

	group.Get("/position-preferences", sc.PositionPreferencesPage)
	group.Get("/position-preferences/assignments", sc.PositionAssignments)
	group.Post("/position-preferences", sc.SetPositionPreference)
	group.Delete("/position-preferences/:team_id/:team_position_id/:assignment_id", sc.DeletePositionPreference)

	group.Get("/build", sc.BuildPage)
	group.Get("/build/plans", sc.BuildPlans)
	group.Get("/build/plans/search", sc.BuildPlansSearch)
	group.Get("/build/:service_type_id/:plan_id", sc.BuildPlanPage)

	group.Post("/rosters/:service_type_id/:plan_id/assignments", sc.AddRosterAssignment)
	group.Delete("/rosters/:service_type_id/:plan_id/assignments/:assignment_id", sc.RemoveRosterAssignment)
	group.Post("/rosters/:service_type_id/:plan_id/publish", sc.PublishRoster)
	group.Post("/rosters/:service_type_id/:plan_id/revert", sc.RevertRoster)

	group.Post("/sets/:service_type_id/:plan_id/songs", sc.AddSetSong)
	group.Delete("/sets/:service_type_id/:plan_id/set-songs/:set_song_id", sc.RemoveSetSong)
	group.Post("/sets/:service_type_id/:plan_id/set-songs/:set_song_id/replace", sc.ReplaceSetSong)
	group.Post("/sets/:service_type_id/:plan_id/set-songs/:set_song_id/reposition", sc.RepositionSetSong)
	group.Post("/sets/:service_type_id/:plan_id/set-songs/:set_song_id/duplicate", sc.DuplicateSetSong)
	group.Post("/sets/:service_type_id/:plan_id/publish", sc.PublishSet)
	group.Post("/sets/:service_type_id/:plan_id/revert", sc.RevertSet)
}

func (sc *SchedulingController) RelationshipsPage(c *fiber.Ctx) error {
	_, personName := middleware.CurrentPerson(c)
	orgName := middleware.CurrentOrganization(c)

	songs, songsErr := fetchAllSongs(middleware.RequestContext(c))

	relationshipTypes, err := sc.store.ListSongRelationshipTypes(c.Context())
	if err != nil {
		return render(c, views.Relationships(personName, orgName, songs, nil, nil, songTitleMap(songs), nil, err.Error()))
	}

	relationships, err := sc.store.ListSongRelationships(c.Context(), "")
	if err != nil {
		return render(c, views.Relationships(personName, orgName, songs, nil, relationshipTypes, songTitleMap(songs), songRelationshipTypeLabelMap(relationshipTypes), err.Error()))
	}

	errMsg := ""
	if songsErr != nil {
		errMsg = "loading songs from Planning Center: " + songsErr.Error()
	}

	return render(c, views.Relationships(personName, orgName, songs, relationships, relationshipTypes, songTitleMap(songs), songRelationshipTypeLabelMap(relationshipTypes), errMsg))
}

func (sc *SchedulingController) CreateRelationship(c *fiber.Ctx) error {
	var body scheduling.CreateSongRelationshipParams
	errMsg := ""
	if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else if id, editing, idErr := parseFormID(c); idErr != nil {
		errMsg = "id must be an integer"
	} else if editing {
		if _, err := sc.store.UpdateSongRelationship(c.Context(), id, body); err != nil {
			errMsg = err.Error()
		}
	} else if _, err := sc.store.CreateSongRelationship(c.Context(), body); err != nil {
		errMsg = err.Error()
	}

	relationships, err := sc.store.ListSongRelationships(c.Context(), "")
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}

	// Best-effort: song titles and relationship type labels are a display
	// nicety for the table, so a failure here just falls back to showing
	// raw IDs/codes rather than failing the whole request.
	songs, _ := fetchAllSongs(middleware.RequestContext(c))
	relationshipTypes, _ := sc.store.ListSongRelationshipTypes(c.Context())

	return render(c, views.RelationshipsTable(relationships, songTitleMap(songs), songRelationshipTypeLabelMap(relationshipTypes), errMsg))
}

func (sc *SchedulingController) DeleteRelationship(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	errMsg := ""
	if err != nil {
		errMsg = "id must be an integer"
	} else if delErr := sc.store.DeleteSongRelationship(c.Context(), int64(id)); delErr != nil && !errors.Is(delErr, sql.ErrNoRows) {
		errMsg = delErr.Error()
	}

	relationships, err := sc.store.ListSongRelationships(c.Context(), "")
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}

	songs, _ := fetchAllSongs(middleware.RequestContext(c))
	relationshipTypes, _ := sc.store.ListSongRelationshipTypes(c.Context())

	return render(c, views.RelationshipsTable(relationships, songTitleMap(songs), songRelationshipTypeLabelMap(relationshipTypes), errMsg))
}

func (sc *SchedulingController) PreferencesPage(c *fiber.Ctx) error {
	_, personName := middleware.CurrentPerson(c)
	orgName := middleware.CurrentOrganization(c)

	ctx := middleware.RequestContext(c)
	songs, songsErr := fetchAllSongs(ctx)

	preferenceTypes, err := sc.store.ListSongPreferenceTypes(c.Context())
	if err != nil {
		return render(c, views.Preferences(personName, orgName, songs, nil, nil, songTitleMap(songs), nil, nil, err.Error()))
	}

	preferences, err := sc.store.ListSongPersonPreferences(c.Context(), "", "")
	if err != nil {
		return render(c, views.Preferences(personName, orgName, songs, nil, preferenceTypes, songTitleMap(songs), nil, songPreferenceTypeLabelMap(preferenceTypes), err.Error()))
	}

	errMsg := ""
	if songsErr != nil {
		errMsg = "loading songs from Planning Center: " + songsErr.Error()
	}

	return render(c, views.Preferences(personName, orgName, songs, preferences, preferenceTypes, songTitleMap(songs), personNameMap(ctx, preferences), songPreferenceTypeLabelMap(preferenceTypes), errMsg))
}

func (sc *SchedulingController) SetPreference(c *fiber.Ctx) error {
	var body scheduling.SetSongPersonPreferenceParams
	errMsg := ""
	if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else if id, editing, idErr := parseFormID(c); idErr != nil {
		errMsg = "id must be an integer"
	} else if editing {
		if _, err := sc.store.UpdateSongPersonPreference(c.Context(), id, body); err != nil {
			errMsg = err.Error()
		}
	} else if _, err := sc.store.SetSongPersonPreference(c.Context(), body); err != nil {
		errMsg = err.Error()
	}

	preferences, err := sc.store.ListSongPersonPreferences(c.Context(), "", "")
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}

	// Best-effort: song/person display names and preference type labels are
	// a table nicety, so a lookup failure just falls back to raw IDs/codes
	// rather than failing the whole request.
	ctx := middleware.RequestContext(c)
	songs, _ := fetchAllSongs(ctx)
	preferenceTypes, _ := sc.store.ListSongPreferenceTypes(c.Context())

	return render(c, views.PreferencesTable(preferences, songTitleMap(songs), personNameMap(ctx, preferences), songPreferenceTypeLabelMap(preferenceTypes), errMsg))
}

func (sc *SchedulingController) DeletePreference(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	errMsg := ""
	if err != nil {
		errMsg = "id must be an integer"
	} else if delErr := sc.store.DeleteSongPersonPreference(c.Context(), int64(id)); delErr != nil && !errors.Is(delErr, sql.ErrNoRows) {
		errMsg = delErr.Error()
	}

	preferences, err := sc.store.ListSongPersonPreferences(c.Context(), "", "")
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}

	ctx := middleware.RequestContext(c)
	songs, _ := fetchAllSongs(ctx)
	preferenceTypes, _ := sc.store.ListSongPreferenceTypes(c.Context())

	return render(c, views.PreferencesTable(preferences, songTitleMap(songs), personNameMap(ctx, preferences), songPreferenceTypeLabelMap(preferenceTypes), errMsg))
}

// PeopleSearch backs the person picker's search-as-you-type on the
// preferences page: ?q= is matched against PCO's search_name_or_email
// filter (fuzzy, name or email) via PeopleParams.Email - see its doc
// comment. An empty query returns no results rather than paging through the
// whole directory.
func (sc *SchedulingController) PeopleSearch(c *fiber.Ctx) error {
	q := strings.TrimSpace(c.Query("q"))
	suffix := c.Query("suffix")
	if q == "" {
		return render(c, views.PersonSearchResults(nil, suffix))
	}

	people, err := pco.GetPeople(middleware.RequestContext(c), &pco.PeopleParams{
		Email:   q,
		OrderBy: "last_name",
		PerPage: personSearchResultLimit,
	})
	if err != nil {
		return render(c, views.PersonSearchResults(nil, suffix))
	}

	return render(c, views.PersonSearchResults(people.Data, suffix))
}
