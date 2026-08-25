package controllers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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

// formatArrangementInfo renders a cached (or freshly-fetched) arrangement
// summary as a single "G · 72 BPM · 4/4 · 3:45" display string, skipping
// whichever fields are blank/zero - "" when every field is, so the caller
// just omits the line rather than showing something empty.
func formatArrangementInfo(info scheduling.SongArrangementInfo) string {
	var parts []string
	if info.Key != "" {
		parts = append(parts, info.Key)
	}
	if info.BPM != 0 {
		parts = append(parts, fmt.Sprintf("%g BPM", info.BPM))
	}
	if info.Meter != "" {
		parts = append(parts, info.Meter)
	}
	if info.LengthSeconds != 0 {
		parts = append(parts, fmt.Sprintf("%d:%02d", info.LengthSeconds/60, info.LengthSeconds%60))
	}

	return strings.Join(parts, " · ")
}

// fetchAndCacheArrangementInfo live-fetches songID's arrangement from
// Planning Center and upserts it into song_arrangement_cache, returning
// the resolved info - or an error, and no write, if the fetch failed or
// the song genuinely has no arrangement at all (PCO auto-creates one for
// every song via the API, so this should be rare in practice). Shared by
// resolveArrangementInfo's cache-miss path and SyncSongInfo's bulk
// backfill (song_arrangement_cache.go), so there's exactly one place that
// knows how to turn a PCO arrangement into a cache row.
func fetchAndCacheArrangementInfo(ctx context.Context, store *scheduling.Store, organizationID, songID string) (scheduling.SongArrangementInfo, error) {
	resp, err := pco.GetArrangements(ctx, songID, &pco.ArrangementsParams{PerPage: 1})
	if err != nil {
		return scheduling.SongArrangementInfo{}, err
	}
	if len(resp.Data) == 0 {
		return scheduling.SongArrangementInfo{}, fmt.Errorf("song %s has no arrangement", songID)
	}

	attrs := resp.Data[0].Attributes
	info := scheduling.SongArrangementInfo{
		Key:           attrs.ChordChartKey,
		BPM:           attrs.BPM,
		Meter:         attrs.Meter,
		LengthSeconds: attrs.Length,
	}

	if err := store.UpsertSongArrangementInfo(ctx, organizationID, songID, info); err != nil {
		return info, err
	}

	return info, nil
}

// resolveArrangementInfo returns songID's display-ready arrangement info,
// preferring the already-loaded cache (zero Planning Center calls) and
// falling back to fetchAndCacheArrangementInfo on a miss. This is the
// "song-by-song" catch-up the cache relies on: a song added to a library
// after the last sync (or never synced at all) still shows correct info
// the first time it's actually rendered, same as before this cache
// existed, and every render after that is a cache hit. Best-effort: a
// failed fetch/backfill just means "" for this render and another live
// attempt next time, not a broken page.
func resolveArrangementInfo(ctx context.Context, store *scheduling.Store, organizationID string, cache map[string]scheduling.SongArrangementInfo, songID string) string {
	if info, ok := cache[songID]; ok {
		return formatArrangementInfo(info)
	}

	info, err := fetchAndCacheArrangementInfo(ctx, store, organizationID, songID)
	if err != nil {
		return ""
	}

	return formatArrangementInfo(info)
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
// when editing an existing row (see editRow in web/static/js/scheduling.js,
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

	group.Get("/song-tag-suggestions", sc.SongTagSuggestionsPage)
	group.Post("/song-tag-suggestions/apply", sc.ApplySongTagSuggestion)
	group.Post("/song-tag-suggestions/ignore", sc.IgnoreSongTagSuggestion)

	group.Get("/song-info", sc.SongInfoPage)
	group.Post("/song-info/sync", sc.SyncSongInfo)

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

	// Quick-add: the same setup-level entities as the dedicated setup pages
	// above (song tags/relationships/roles, person relationships, position
	// preferences), posted from within the build workflow instead - see
	// QuickAdd* in sets.go/rosters.go for why these are separate handlers
	// rather than reusing the setup-page POSTs above.
	group.Post("/build/:service_type_id/:plan_id/song-tags", sc.QuickAddSongTag)
	group.Post("/build/:service_type_id/:plan_id/song-relationships", sc.QuickAddSongRelationship)
	group.Post("/build/:service_type_id/:plan_id/song-roles", sc.QuickAddSongRole)
	group.Post("/build/:service_type_id/:plan_id/person-relationships", sc.QuickAddPersonRelationship)
	group.Post("/build/:service_type_id/:plan_id/position-preferences", sc.QuickAddPositionPreference)

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
	orgID := middleware.CurrentOrganizationID(c)

	songs, songsErr := fetchAllSongs(middleware.RequestContext(c))

	relationshipTypes, err := sc.store.ListSongRelationshipTypes(c.Context())
	if err != nil {
		return render(c, views.Relationships(personName, orgName, songs, nil, nil, songTitleMap(songs), nil, err.Error()))
	}

	relationships, err := sc.store.ListSongRelationships(c.Context(), orgID, "")
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
	orgID := middleware.CurrentOrganizationID(c)

	var body scheduling.CreateSongRelationshipParams
	errMsg := ""
	if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else if id, editing, idErr := parseFormID(c); idErr != nil {
		errMsg = "id must be an integer"
	} else if editing {
		if _, err := sc.store.UpdateSongRelationship(c.Context(), orgID, id, body); err != nil {
			errMsg = err.Error()
		}
	} else if _, err := sc.store.CreateSongRelationship(c.Context(), orgID, body); err != nil {
		errMsg = err.Error()
	}

	relationships, err := sc.store.ListSongRelationships(c.Context(), orgID, "")
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
	orgID := middleware.CurrentOrganizationID(c)

	id, err := c.ParamsInt("id")
	errMsg := ""
	if err != nil {
		errMsg = "id must be an integer"
	} else if delErr := sc.store.DeleteSongRelationship(c.Context(), orgID, int64(id)); delErr != nil && !errors.Is(delErr, sql.ErrNoRows) {
		errMsg = delErr.Error()
	}

	relationships, err := sc.store.ListSongRelationships(c.Context(), orgID, "")
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
	orgID := middleware.CurrentOrganizationID(c)

	ctx := middleware.RequestContext(c)
	songs, songsErr := fetchAllSongs(ctx)

	preferenceTypes, err := sc.store.ListSongPreferenceTypes(c.Context())
	if err != nil {
		return render(c, views.Preferences(personName, orgName, songs, nil, nil, songTitleMap(songs), nil, nil, err.Error()))
	}

	preferences, err := sc.store.ListSongPersonPreferences(c.Context(), orgID, "", "")
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
	orgID := middleware.CurrentOrganizationID(c)

	var body scheduling.SetSongPersonPreferenceParams
	errMsg := ""
	if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else if id, editing, idErr := parseFormID(c); idErr != nil {
		errMsg = "id must be an integer"
	} else if editing {
		if _, err := sc.store.UpdateSongPersonPreference(c.Context(), orgID, id, body); err != nil {
			errMsg = err.Error()
		}
	} else if _, err := sc.store.SetSongPersonPreference(c.Context(), orgID, body); err != nil {
		errMsg = err.Error()
	}

	preferences, err := sc.store.ListSongPersonPreferences(c.Context(), orgID, "", "")
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
	orgID := middleware.CurrentOrganizationID(c)

	id, err := c.ParamsInt("id")
	errMsg := ""
	if err != nil {
		errMsg = "id must be an integer"
	} else if delErr := sc.store.DeleteSongPersonPreference(c.Context(), orgID, int64(id)); delErr != nil && !errors.Is(delErr, sql.ErrNoRows) {
		errMsg = delErr.Error()
	}

	preferences, err := sc.store.ListSongPersonPreferences(c.Context(), orgID, "", "")
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
