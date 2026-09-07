package controllers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"pco-assistant/internal/logger"
	"pco-assistant/internal/scheduling"
	"pco-assistant/internal/server/middleware"
	views "pco-assistant/internal/views/scheduling"

	pco "github.com/drkgrntt/go-pco"
	"github.com/gofiber/fiber/v2"
)

// personSearchResultLimit caps how many matches the person picker's
// search-as-you-type shows at once. Unlike songs, a People directory is
// often far too large to fetch in full, so people are looked up on demand
// by search query instead of listed up front.
const personSearchResultLimit = 8

// fetchActiveSongs pages through the PCO song library, ordered by title,
// excluding archived (hidden) songs, for use as a picker's option list
// (song relationships, song/person preferences) or candidate pool.
// Delegates to scheduling.FetchActiveSongs, which is cached per
// organization for scheduling.songCacheTTL and lives in internal/scheduling
// so middleware.PopulateNavState can fetch the same library without a
// controllers/middleware import cycle - kept as a thin wrapper here since
// it's called throughout this package already.
func fetchActiveSongs(ctx context.Context, organizationID string) ([]pco.SongData, error) {
	return scheduling.FetchActiveSongs(ctx, organizationID)
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

// formatSongStructure renders an arrangement's section-by-section
// structure as a single ", "-joined display string ("Intro, Verse 1,
// Chorus...") - preferring sequenceShort (PCO's abbreviated,
// run-length-collapsed form, e.g. "Intro×2, V1, C") when it's non-empty,
// falling back to the full sequence otherwise. Both come back empty for an
// arrangement PCO hasn't had a structure entered for at all, in which case
// this returns "" - same "just omit the line" convention as
// formatArrangementInfo's own blank/zero-field handling.
func formatSongStructure(sequenceShort, sequence []string) string {
	if len(sequenceShort) > 0 {
		return strings.Join(sequenceShort, ", ")
	}
	return strings.Join(sequence, ", ")
}

// songAttachmentViews converts a song's cached attachments into their
// display-ready view form (views.SongAttachmentView) - a thin field-by-field
// copy, kept as its own function (rather than inlined at the one call site
// in renderServiceOrder) purely to mirror how every other SetSongView field
// derived from a cache lookup gets its own small conversion step in this
// file (formatArrangementInfo, formatSongStructure). nil in, nil out - a
// song with no cached attachments renders no "Files" block at all (see
// songDetailCard).
func songAttachmentViews(infos []scheduling.SongAttachmentInfo) []views.SongAttachmentView {
	if len(infos) == 0 {
		return nil
	}

	out := make([]views.SongAttachmentView, len(infos))
	for i, info := range infos {
		out[i] = views.SongAttachmentView{
			PCOAttachmentID: info.PCOAttachmentID,
			Filename:        info.Filename,
		}
	}
	return out
}

// includedKey is the Key equivalent of includedTeam/includedPlan
// (rosters_builder.go) - just enough of a Key resource's shape (see
// go-pco's KeyData) to read its id and starting key out of a
// GetArrangements response's Included array when fetched with
// Include: []string{"keys"}.
type includedKey struct {
	Type       string `json:"type"`
	ID         string `json:"id"`
	Attributes struct {
		StartingKey string `json:"starting_key"`
	} `json:"attributes"`
}

// keysFromIncluded extracts Key entries from a GetArrangements response's
// Included array, mirroring teamNamesFromIncluded/planSortDatesFromIncluded
// (rosters_builder.go). Any entry that isn't a well-formed Key is silently
// skipped. Returned in Included's own order, since
// fetchAndCacheArrangementInfo only ever reads the first entry - the same
// "first row wins" behavior the former direct
// GetKeys(..., &pco.KeysParams{PerPage: 1}) call had.
func keysFromIncluded(included []any) []includedKey {
	var keys []includedKey
	for _, raw := range included {
		b, err := json.Marshal(raw)
		if err != nil {
			continue
		}
		var k includedKey
		if err := json.Unmarshal(b, &k); err != nil {
			continue
		}
		if k.Type != "Key" || k.ID == "" {
			continue
		}
		keys = append(keys, k)
	}
	return keys
}

// fetchAndCacheArrangementInfo live-fetches songID's arrangement from
// Planning Center and upserts it into song_arrangement_cache, returning
// the resolved info - or an error, and no write, if the fetch failed or
// the song genuinely has no arrangement at all (PCO auto-creates one for
// every song via the API, so this should be rare in practice). Shared by
// runSongInfoQueueSync's background queue-drain sync and
// RefreshSetSongArrangementInfo's manual single-song refresh
// (song_arrangement_cache.go), so there's exactly one place that knows how
// to turn a PCO arrangement into a cache row. resolveArrangementCacheInfo's
// own cache-miss path (below) deliberately does NOT call this directly -
// see its own doc comment for why it enqueues instead.
//
// Also fetches and caches this song's Attachments (fetchAndCacheSongAttachments
// below) as part of the same "sync this song's info" unit - see
// docs/plans/shipped/song-lead-sheet-attachments-plan.md. That's a second,
// independent PCO call (this function/its callers now cost 2 live PCO
// calls per uncached song, not 1 - see songInfoSyncBatchPause's own doc
// comment for the pacing math this feeds), kept best-effort and never
// reflected in this function's own returned error: a hiccup fetching
// attachments shouldn't make an otherwise-successful arrangement fetch look
// like a failed sync to callers that gate real behavior on that error (see
// fetchAndCacheSongAttachments' own doc comment for why).
//
// Key comes from the arrangement's Key sub-resource (its StartingKey),
// not the arrangement's own ChordChartKey attribute - confirmed live
// against real songs: ChordChartKey only ever gets set when there's an
// actual chord chart document attached (HasChordChart true), which is the
// unusual case; picking a plain key via Planning Center's simple key
// picker (the common case) creates a Key row instead and leaves
// ChordChartKey blank. ChordChartKey is used only as a fallback for the
// rarer case where a chord chart's own key is set but no Key row exists.
func fetchAndCacheArrangementInfo(ctx context.Context, store *scheduling.Store, organizationID, songID string) (scheduling.SongArrangementInfo, error) {
	resp, err := pco.GetArrangements(ctx, songID, &pco.ArrangementsParams{PerPage: 1, Include: []string{"keys"}})
	if err != nil {
		return scheduling.SongArrangementInfo{}, err
	}
	if len(resp.Data) == 0 {
		return scheduling.SongArrangementInfo{}, fmt.Errorf("song %s has no arrangement", songID)
	}

	arrangement := resp.Data[0]
	attrs := arrangement.Attributes
	key := attrs.ChordChartKey
	keyID := ""
	if keys := keysFromIncluded(resp.Included); len(keys) > 0 && keys[0].Attributes.StartingKey != "" {
		key = keys[0].Attributes.StartingKey
		// A genuinely separate relationship from the arrangement, not
		// implied by it (confirmed live: an item with only its
		// arrangement linked still shows no key at all) - only set when
		// there's a real Key row to point at, same condition as the
		// display string above, so the two stay consistent with each
		// other.
		keyID = keys[0].ID
	}

	info := scheduling.SongArrangementInfo{
		Key:           key,
		BPM:           attrs.BPM,
		Meter:         attrs.Meter,
		LengthSeconds: attrs.Length,
		ArrangementID: arrangement.ID,
		Notes:         attrs.Notes,
		KeyID:         keyID,
		Structure:     formatSongStructure(attrs.SequenceShort, attrs.Sequence),
		HasChordChart: attrs.HasChordChart,
	}

	if err := store.UpsertSongArrangementInfo(ctx, organizationID, songID, info); err != nil {
		return info, err
	}

	fetchAndCacheSongAttachments(ctx, store, organizationID, songID)

	return info, nil
}

// fetchAndCacheSongAttachments live-fetches songID's Planning Center
// Attachments (lead sheet/chord chart PDFs, scans, audio - arbitrary
// uploaded files, see pco.GetAttachments) and replaces its cached
// attachment list (scheduling.Store.UpsertSongAttachments) - the
// attachment half of fetchAndCacheArrangementInfo's "sync this song's
// info" unit above, called from there rather than from each of that
// function's own call sites.
//
// Deliberately swallows its own errors (logged, not returned): this is a
// newer, less-established PCO call than the arrangement fetch it rides
// alongside, and several of fetchAndCacheArrangementInfo's callers
// (sets_publish.go's publish-time arrangement resolution, in particular)
// use that function's returned error to decide whether the *arrangement*
// info it just fetched is safe to use - an unrelated attachments hiccup
// must never make those callers discard a perfectly good, already-persisted
// arrangement fetch.
func fetchAndCacheSongAttachments(ctx context.Context, store *scheduling.Store, organizationID, songID string) {
	resp, err := pco.GetAttachments(ctx, songID, nil)
	if err != nil {
		logger.WarnContext(ctx, "song info sync: fetching attachments failed", "organization_id", organizationID, "song_id", songID, "error", err)
		return
	}

	attachments := make([]scheduling.SongAttachmentInfo, 0, len(resp.Data))
	for _, a := range resp.Data {
		attachments = append(attachments, scheduling.SongAttachmentInfo{
			PCOAttachmentID: a.ID,
			Filename:        a.Attributes.Filename,
			ContentType:     a.Attributes.ContentType,
		})
	}

	if err := store.UpsertSongAttachments(ctx, organizationID, songID, attachments); err != nil {
		logger.WarnContext(ctx, "song info sync: caching attachments failed", "organization_id", organizationID, "song_id", songID, "error", err)
	}
}

// itemDescriptionFor resolves what to publish as a set song's plan item
// Description: its own local per-occurrence note (SetSong.Description) when
// there is one, falling back to the song's persistent arrangement notes
// (info.Notes) otherwise - the same default this app has used since before
// per-occurrence notes were editable, just no longer overriding a note
// someone's actually set for this specific week.
func itemDescriptionFor(ss scheduling.SetSong, info scheduling.SongArrangementInfo) string {
	if ss.Description != "" {
		return ss.Description
	}
	return info.Notes
}

// effectiveKey is the display/edit value for views.SetSongView.Key - the
// local override when set, falling back to the arrangement's own cached
// default key, same convention as itemDescriptionFor above.
func effectiveKey(ss scheduling.SetSong, info scheduling.SongArrangementInfo) string {
	if ss.KeyOverride != "" {
		return ss.KeyOverride
	}
	return info.Key
}

// resolveKeyID resolves ss.KeyOverride ("put this in Bb this week") to a
// real Planning Center Key id, live-fetching or creating it under the
// song's own arrangement as needed - falling back to the arrangement's
// cached default Key id (defaultKeyID) when there's no override, same
// convention as itemDescriptionFor above. Never touches the arrangement's
// own default key: a match under it is reused, a new one is added
// alongside it, so every plan not overriding this song keeps seeing
// whatever key it always has.
//
// Matched case-insensitively by Key.StartingKey against the override
// string verbatim - including any trailing "m" someone types for a minor
// key (e.g. "Em"). PCO's own StartingMinor flag can't be set through the
// API at all (confirmed live: "starting_minor cannot be assigned"), so a
// minor key only round-trips correctly through this app if it's created
// and always re-matched here, not by relying on PCO's own minor-key
// rendering - typing the same string back finds the same Key row again
// rather than creating a duplicate.
//
// Returns a non-nil err whenever a real override couldn't actually be
// resolved (no arrangement to attach a key under, or the create-under-
// arrangement call itself failed) - keyID is still the best available
// fallback (defaultKeyID) in that case, never "", so every caller can
// keep using the returned id unconditionally and only needs err to decide
// whether to also surface a publish failure. Silently swallowing this
// used to mean a key someone explicitly set here could quietly not make
// it into Planning Center - the item would just keep the arrangement's
// default key with no error anywhere, including on a plain PCO 429 this
// app already has documented history with under load (see
// docs/pco-api-risks.md).
func resolveKeyID(ctx context.Context, songID, arrangementID, keyOverride, defaultKeyID string) (keyID string, err error) {
	if keyOverride == "" {
		return defaultKeyID, nil
	}
	if arrangementID == "" {
		return defaultKeyID, errors.New("no arrangement linked to attach a key to")
	}

	if keys, getErr := pco.GetKeys(ctx, songID, arrangementID, nil); getErr == nil {
		for _, k := range keys.Data {
			if strings.EqualFold(k.Attributes.StartingKey, keyOverride) {
				return k.ID, nil
			}
		}
	}

	created, createErr := pco.CreateKey(ctx, songID, arrangementID, &pco.CreateKeyParams{StartingKey: keyOverride})
	if createErr != nil {
		return defaultKeyID, createErr
	}
	return created.Data.ID, nil
}

// resolveArrangementCacheInfo returns songID's full cached arrangement
// info (not just the formatted display string - see resolveArrangementInfo
// below for that), preferring the already-loaded cache (zero Planning
// Center calls) and never blocking on a miss: a miss enqueues a background
// sync request (enqueueSongSync, song_arrangement_cache.go) and returns a
// zero-value info immediately instead, matching person_data_cache.go's own
// cache-first read paths - see rosters_warnings.go's otherAssignmentDates/
// crossTeamAssignmentsThisWeek/personBlockouts for the pattern this
// mirrors. A song added to a library after the last sync (or never synced
// at all) shows blank arrangement info on the render that first discovers
// it, then correct info on the next - buildSetBuilderData/
// buildSetBuilderDataWithErr (sets_builder.go) trigger the queued sync
// right before they return, so "the next render" typically means "the next
// time this plan/set is opened", not an indefinite wait.
//
// ctx/store are still threaded through - unused on this miss path itself,
// since enqueueSongSync takes neither - so this keeps one signature for
// both branches and for resolveArrangementInfo's own pass-through below;
// the actual PCO fetch a miss used to make inline now happens later, out
// of band, in runSongInfoQueueSync.
//
// Split out from resolveArrangementInfo so a caller needing more than the
// formatted string (e.g. renderServiceOrder's song detail card, which also
// wants the arrangement's raw Notes) can get everything from one lookup.
func resolveArrangementCacheInfo(ctx context.Context, store *scheduling.Store, organizationID string, cache map[string]scheduling.SongArrangementInfo, songID string) scheduling.SongArrangementInfo {
	if info, ok := cache[songID]; ok {
		return info
	}

	enqueueSongSync(songSyncRequest{organizationID: organizationID, songID: songID})
	return scheduling.SongArrangementInfo{}
}

// resolveArrangementInfo returns songID's display-ready arrangement info -
// see resolveArrangementCacheInfo for the cache/fetch behavior this wraps.
func resolveArrangementInfo(ctx context.Context, store *scheduling.Store, organizationID string, cache map[string]scheduling.SongArrangementInfo, songID string) string {
	return formatArrangementInfo(resolveArrangementCacheInfo(ctx, store, organizationID, cache, songID))
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
// it looks up only the handful of IDs actually in use. Just collects those
// distinct IDs and delegates to personNameMapForIDs (people.go), which
// batches them into `where[id]=...` calls of up to personIDBatchSize at
// once rather than one GetPerson round trip per person - this used to do
// exactly that sequential per-person loop itself before personNameMapForIDs
// existed. An ID that fails to resolve (e.g. the person was since deleted)
// is simply left out of the map; callers fall back to showing the raw ID -
// personNameMapForIDs already has this same fallback behavior.
func personNameMap(ctx context.Context, preferences []scheduling.SongPersonPreference) map[string]string {
	seen := make(map[string]bool, len(preferences))
	ids := make([]string, 0, len(preferences))
	for _, pref := range preferences {
		if !seen[pref.PersonID] {
			seen[pref.PersonID] = true
			ids = append(ids, pref.PersonID)
		}
	}
	return personNameMapForIDs(ctx, ids)
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

	group.Get("/person-relationships", sc.PersonRelationshipsPage)
	group.Post("/person-relationships", sc.CreatePersonRelationship)
	group.Delete("/person-relationships/:id", sc.DeletePersonRelationship)

	group.Get("/vocal-range", sc.PersonVocalRangePage)
	group.Post("/vocal-range", sc.SetPersonVocalRange)
	group.Delete("/vocal-range/:id", sc.DeletePersonVocalRange)

	group.Get("/song-vocal-range", sc.SongVocalRangePage)
	group.Post("/song-vocal-range", sc.SetSongVocalRange)
	group.Delete("/song-vocal-range/:id", sc.DeleteSongVocalRange)

	group.Get("/position-preferences", sc.PositionPreferencesPage)
	group.Get("/position-preferences/assignments", sc.PositionAssignments)
	group.Post("/position-preferences", sc.SetPositionPreference)
	group.Delete("/position-preferences/:team_id/:team_position_id/:assignment_id", sc.DeletePositionPreference)

	group.Get("/build", sc.BuildPage)
	group.Get("/build/plans", sc.BuildPlans)
	group.Get("/build/plans/search", sc.BuildPlansSearch)
	group.Post("/build/plans/:service_type_id/:plan_id/note", sc.UpdatePlanNote)
	group.Post("/build/:service_type_id/:plan_id/vocal-note-category", sc.SetVocalNoteCategory)
	group.Get("/build/:service_type_id/:plan_id", sc.BuildPlanPage)
	group.Get("/build/:service_type_id/:plan_id/run-sheet", sc.RunSheetPage)
	group.Get("/build/:service_type_id/:plan_id/songs-tab", sc.BuildSongsTabLazy)
	group.Get("/build/:service_type_id/:plan_id/team-tab", sc.BuildTeamTabLazy)
	group.Get("/build/:service_type_id/:plan_id/candidates/search", sc.BuildCandidatesSearch)
	group.Get("/build/:service_type_id/:plan_id/candidates/:song_id/details", sc.BuildCandidateDetails)
	group.Post("/build/:service_type_id/:plan_id/candidates/:song_id/archive", sc.ArchiveSong)

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
	group.Get("/rosters/:service_type_id/:plan_id/candidates/:person_id/warnings", sc.CandidateWarnings)
	group.Post("/rosters/:service_type_id/:plan_id/assignments/:assignment_id/refresh-data", sc.RefreshPersonData)
	group.Post("/rosters/:service_type_id/:plan_id/publish", sc.PublishRoster)
	group.Post("/rosters/:service_type_id/:plan_id/revert", sc.RevertRoster)
	group.Post("/rosters/:service_type_id/:plan_id/refresh-team-data", sc.RefreshRosterTeamData)

	group.Post("/sets/:service_type_id/:plan_id/songs", sc.AddSetSong)
	group.Delete("/sets/:service_type_id/:plan_id/set-songs/:set_song_id", sc.RemoveSetSong)
	group.Post("/sets/:service_type_id/:plan_id/set-songs/:set_song_id/replace", sc.ReplaceSetSong)
	group.Post("/sets/:service_type_id/:plan_id/set-songs/:set_song_id/reposition", sc.RepositionSetSong)
	group.Post("/sets/:service_type_id/:plan_id/set-songs/:set_song_id/duplicate", sc.DuplicateSetSong)
	group.Post("/sets/:service_type_id/:plan_id/set-songs/:set_song_id/description", sc.UpdateSetSongDescription)
	group.Post("/sets/:service_type_id/:plan_id/set-songs/:set_song_id/key", sc.UpdateSetSongKey)
	group.Post("/sets/:service_type_id/:plan_id/set-songs/:set_song_id/leader", sc.UpdateSetSongLeader)
	group.Post("/sets/:service_type_id/:plan_id/set-songs/:set_song_id/refresh-arrangement-info", sc.RefreshSetSongArrangementInfo)
	group.Get("/sets/:service_type_id/:plan_id/set-songs/:set_song_id/attachments/:attachment_id", sc.OpenSongAttachment)
	group.Post("/sets/:service_type_id/:plan_id/items/:item_id/notes", sc.UpdateSongNote)
	group.Post("/sets/:service_type_id/:plan_id/publish", sc.PublishSet)
	group.Post("/sets/:service_type_id/:plan_id/refresh-info", sc.RefreshSongInfo)
	group.Post("/sets/:service_type_id/:plan_id/revert", sc.RevertSet)
	group.Post("/sets/:service_type_id/:plan_id/merge", sc.MergeSet)
	group.Get("/sets/:service_type_id/:plan_id/sync-check", sc.SyncCheck)
}

func (sc *SchedulingController) RelationshipsPage(c *fiber.Ctx) error {
	_, personName := middleware.CurrentPerson(c)
	orgName := middleware.CurrentOrganization(c)
	orgID := middleware.CurrentOrganizationID(c)

	songs, songsErr := fetchActiveSongs(middleware.RequestContext(c), orgID)

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
	songs, _ := fetchActiveSongs(middleware.RequestContext(c), orgID)
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

	songs, _ := fetchActiveSongs(middleware.RequestContext(c), orgID)
	relationshipTypes, _ := sc.store.ListSongRelationshipTypes(c.Context())

	return render(c, views.RelationshipsTable(relationships, songTitleMap(songs), songRelationshipTypeLabelMap(relationshipTypes), errMsg))
}

func (sc *SchedulingController) PreferencesPage(c *fiber.Ctx) error {
	_, personName := middleware.CurrentPerson(c)
	orgName := middleware.CurrentOrganization(c)
	orgID := middleware.CurrentOrganizationID(c)

	ctx := middleware.RequestContext(c)
	songs, songsErr := fetchActiveSongs(ctx, orgID)

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
	songs, _ := fetchActiveSongs(ctx, orgID)
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
	songs, _ := fetchActiveSongs(ctx, orgID)
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
