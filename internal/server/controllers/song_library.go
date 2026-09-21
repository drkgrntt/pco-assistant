package controllers

// Song library index + song detail page
// (docs/plans/not-started/song-person-detail-pages-plan.md, Phase 1 - song
// side only). A standalone, per-song aggregate view of everything this
// app's own database knows about one song - roles, labels (tags),
// contexts, relationships to other songs, person preferences, vocal
// range, and debrief/talking-point notes - plus the library-level facts
// already cached from Planning Center (arrangement info, structure, chord
// chart, attachments) that songDetailCard already shows on the Build
// page's sidebar. See that plan doc's "Concrete architecture" section for
// the exact shape this file implements.
//
// Deliberately additive only, per that plan's phasing: nothing here
// touches any of the ten existing flat setup pages' own routes/handlers/
// views, and this page isn't linked from navMenuContent yet (that's
// Phase 3, the eventual retirement pass) - reachable today only by direct
// URL (/scheduling/songs).
//
// Every AddSongDetail*/DeleteSongDetail* handler below re-fetches this
// song's whole detail-page dataset fresh (songDetailData) and re-renders
// the single swappable #song-detail-content fragment (renderSongDetail) -
// the same "quick add, re-render the whole [aggregate view]" shape
// QuickAddTransitionNote/QuickAddSongTag already use for the Build page's
// own per-song detail card (transition_notes.go/sets_detail.go), simpler
// and safer here than hand-picking which of this page's many independent
// sections a given mutation could have touched. None of these call any
// existing setup-page handler or store method that doesn't already exist -
// see the plan's own "no store changes are needed at all, just calling
// them with a real id instead of ''" note.

import (
	"context"
	"database/sql"
	"errors"

	"pco-assistant/internal/scheduling"
	"pco-assistant/internal/server/middleware"
	views "pco-assistant/internal/views/scheduling"

	pco "github.com/drkgrntt/go-pco"
	"github.com/gofiber/fiber/v2"
)

// findSongByID looks up songID within an already-fetched active song
// list (fetchActiveSongs) - the song detail page deliberately doesn't make
// its own live pco.GetSong call (see this file's own doc comment and the
// plan's "zero new PCO calls" framing): the org-wide cached list this app
// already fetches everywhere else covers it. ok is false when songID isn't
// (or is no longer) an active song in this organization's library - a
// stale link, or a song archived in Planning Center since this page was
// last linked to.
func findSongByID(songs []pco.SongData, songID string) (pco.SongData, bool) {
	for _, s := range songs {
		if s.ID == songID {
			return s, true
		}
	}
	return pco.SongData{}, false
}

// songsExcluding returns every song except excludeID - the option list for
// the detail page's "relate to another song" picker, which shouldn't offer
// the song whose own page it's on.
func songsExcluding(songs []pco.SongData, excludeID string) []pco.SongData {
	out := make([]pco.SongData, 0, len(songs))
	for _, s := range songs {
		if s.ID != excludeID {
			out = append(out, s)
		}
	}
	return out
}

// songRolesBySongID groups a flat song-role list by song id - the library
// index card's own per-song role-badge lookup, and the song detail page's
// own per-song role list. The song-keyed counterpart of songRolesByRoleCode
// (sets_roles.go), which groups the same data by role code instead for the
// Song Roles bucket page (and this index's own "browse by role" section,
// reused as-is - see songLibraryData below).
func songRolesBySongID(roles []scheduling.SongRole) map[string][]scheduling.SongRole {
	bySong := make(map[string][]scheduling.SongRole, len(roles))
	for _, r := range roles {
		bySong[r.SongID] = append(bySong[r.SongID], r)
	}
	return bySong
}

// songLibraryData gathers every organization-wide read the library index
// page needs, in one place - mirrors buildSetBuilderData's own "one
// function assembles the whole page's data" shape, just far smaller. Every
// read here is the same cheap, org-wide, local-DB-or-cached-PCO call an
// existing flat setup page already makes (fetchActiveSongs,
// ListSongTags/ListSongContext/ListSongRoles with songID ""), so this adds
// no new PCO cost - see docs/plans/not-started/song-person-detail-pages-plan.md's
// "Song side" section.
func (sc *SchedulingController) songLibraryData(ctx context.Context, organizationID string) (views.SongLibraryData, string) {
	songs, songsErr := fetchActiveSongs(ctx, organizationID)

	tags, tagsErr := sc.store.ListSongTags(ctx, organizationID, "", "")
	categories, categoriesErr := sc.store.ListSongTagCategories(ctx)

	contexts, contextsErr := sc.store.ListSongContext(ctx, organizationID, "")
	contextTypes, contextTypesErr := sc.store.ListSongContextTypes(ctx, organizationID)

	roles, rolesErr := sc.store.ListSongRoles(ctx, organizationID, "", "")
	roleTypes, roleTypesErr := sc.store.ListSongRoleTypes(ctx)

	errMsg := ""
	if songsErr != nil {
		errMsg = appendErr(errMsg, "loading songs from Planning Center: "+songsErr.Error())
	}
	for _, e := range []error{tagsErr, categoriesErr, contextsErr, contextTypesErr, rolesErr, roleTypesErr} {
		if e != nil {
			errMsg = appendErr(errMsg, e.Error())
		}
	}

	return views.SongLibraryData{
		Songs:             songs,
		SongTitles:        songTitleMap(songs),
		TagsBySong:        songTagsBySong(tags),
		TagCategoryLabels: songTagCategoryLabelMap(categories),
		ContextsBySong:    songContextsBySong(contexts),
		ContextLabels:     contextTypeLabelMap(contextTypes),
		RolesBySong:       songRolesBySongID(roles),
		RoleLabels:        songRoleTypeLabelMap(roleTypes),
		RoleTypes:         roleTypes,
		RolesByRoleCode:   songRolesByRoleCode(roles),
	}, errMsg
}

// SongLibraryPage is the song library index (GET /scheduling/songs) - see
// this file's own doc comment.
func (sc *SchedulingController) SongLibraryPage(c *fiber.Ctx) error {
	_, personName := middleware.CurrentPerson(c)
	orgName := middleware.CurrentOrganization(c)
	orgID := middleware.CurrentOrganizationID(c)
	ctx := middleware.RequestContext(c)

	data, errMsg := sc.songLibraryData(ctx, orgID)

	return render(c, views.SongLibraryIndex(personName, orgName, data, errMsg))
}

// songDetailData gathers everything one song's detail page shows, fresh,
// for both the initial page load (SongDetailPage) and every mutation
// handler's re-render (renderSongDetail) - see this file's own doc comment
// for why every mutation re-fetches the whole thing rather than patching
// one section. found is false when songID isn't an active song in this
// organization's library (see findSongByID) - callers should respond 404
// rather than trying to render a page with no song to show.
//
// Arrangement info/structure/chord-chart/attachments come from the same
// org-wide cached maps songDetailCard already reads
// (Store.SongArrangementInfo/SongAttachments) - a cache miss enqueues a
// background sync (enqueueSongSync) exactly like resolveArrangementCacheInfo's
// own cache-miss path (scheduling.go) does for the Build page, so this
// page self-heals the same way rather than blocking on a live PCO call.
func (sc *SchedulingController) songDetailData(ctx context.Context, organizationID, songID string) (views.SongDetailData, bool, string) {
	songs, songsErr := fetchActiveSongs(ctx, organizationID)
	song, found := findSongByID(songs, songID)
	if !found {
		return views.SongDetailData{}, false, "that song wasn't found in your active Planning Center song library"
	}

	errMsg := ""
	if songsErr != nil {
		errMsg = appendErr(errMsg, "loading songs from Planning Center: "+songsErr.Error())
	}

	arrangementMap, err := sc.store.SongArrangementInfo(ctx, organizationID)
	if err != nil {
		errMsg = appendErr(errMsg, err.Error())
	}
	arrangement := arrangementMap[songID]
	if _, cached := arrangementMap[songID]; !cached {
		enqueueSongSync(songSyncRequest{organizationID: organizationID, songID: songID})
	}

	attachmentMap, err := sc.store.SongAttachments(ctx, organizationID)
	if err != nil {
		errMsg = appendErr(errMsg, err.Error())
	}

	roles, err := sc.store.ListSongRoles(ctx, organizationID, songID, "")
	if err != nil {
		errMsg = appendErr(errMsg, err.Error())
	}
	roleTypes, _ := sc.store.ListSongRoleTypes(ctx)

	tags, err := sc.store.ListSongTags(ctx, organizationID, songID, "")
	if err != nil {
		errMsg = appendErr(errMsg, err.Error())
	}
	tagCategories, _ := sc.store.ListSongTagCategories(ctx)

	contexts, err := sc.store.ListSongContext(ctx, organizationID, songID)
	if err != nil {
		errMsg = appendErr(errMsg, err.Error())
	}
	contextTypes, _ := sc.store.ListSongContextTypes(ctx, organizationID)

	relationships, err := sc.store.ListSongRelationships(ctx, organizationID, songID)
	if err != nil {
		errMsg = appendErr(errMsg, err.Error())
	}
	relationshipTypes, _ := sc.store.ListSongRelationshipTypes(ctx)

	preferences, err := sc.store.ListSongPersonPreferences(ctx, organizationID, songID, "")
	if err != nil {
		errMsg = appendErr(errMsg, err.Error())
	}
	preferenceTypes, _ := sc.store.ListSongPreferenceTypes(ctx)

	vocalRange, vrErr := sc.store.SongVocalRange(ctx, organizationID, songID)
	hasVocalRange := vrErr == nil
	if vrErr != nil && !errors.Is(vrErr, sql.ErrNoRows) {
		errMsg = appendErr(errMsg, vrErr.Error())
	}

	notes, err := sc.store.ListTransitionNotesForSong(ctx, organizationID, songID)
	if err != nil {
		errMsg = appendErr(errMsg, err.Error())
	}

	songTitles := songTitleMap(songs)

	data := views.SongDetailData{
		Song:                song,
		SongID:              songID,
		ArrangementInfoText: formatArrangementInfo(arrangement),
		Structure:           arrangement.Structure,
		HasChordChart:       arrangement.HasChordChart,
		Attachments:         songAttachmentViews(attachmentMap[songID]),

		Roles:      roles,
		RoleTypes:  roleTypes,
		RoleLabels: songRoleTypeLabelMap(roleTypes),

		Tags:              tags,
		TagCategories:     tagCategories,
		TagCategoryLabels: songTagCategoryLabelMap(tagCategories),

		Contexts:      contexts,
		ContextTypes:  contextTypes,
		ContextLabels: contextTypeLabelMap(contextTypes),

		Relationships:          relationships,
		RelationshipTypes:      relationshipTypes,
		RelationshipTypeLabels: songRelationshipTypeLabelMap(relationshipTypes),
		OtherSongs:             songsExcluding(songs, songID),
		SongTitles:             songTitles,

		Preferences:          preferences,
		PreferenceTypes:      preferenceTypes,
		PreferenceTypeLabels: songPreferenceTypeLabelMap(preferenceTypes),
		PersonNames:          personNameMap(ctx, preferences),

		VocalRange:    vocalRange,
		HasVocalRange: hasVocalRange,

		TransitionNotes: buildTransitionNoteViews(notes, songID, songTitles),
		AllSongs:        songs,
	}

	return data, true, errMsg
}

// SongDetailPage is one song's aggregate detail page (GET
// /scheduling/songs/:song_id) - see this file's own doc comment.
func (sc *SchedulingController) SongDetailPage(c *fiber.Ctx) error {
	_, personName := middleware.CurrentPerson(c)
	orgName := middleware.CurrentOrganization(c)
	orgID := middleware.CurrentOrganizationID(c)
	ctx := middleware.RequestContext(c)
	songID := c.Params("song_id")
	defer triggerSongInfoQueueSync(ctx, sc.store, orgID)

	data, found, errMsg := sc.songDetailData(ctx, orgID, songID)
	if !found {
		return c.Status(fiber.StatusNotFound).SendString(errMsg)
	}

	return render(c, views.SongDetailPage(personName, orgName, data, errMsg))
}

// renderSongDetail is the shared htmx-mutation response for every
// AddSongDetail*/DeleteSongDetail* handler below - see this file's own doc
// comment for why every one of them re-fetches songDetailData fresh and
// re-renders the whole #song-detail-content fragment (views.SongDetailContent)
// rather than a narrower piece. mutationErr, when non-"", is this
// specific mutation's own failure (e.g. a bad id, a store validation
// error) and is shown ahead of anything songDetailData itself failed to
// load.
func (sc *SchedulingController) renderSongDetail(c *fiber.Ctx, songID, mutationErr string) error {
	orgID := middleware.CurrentOrganizationID(c)
	ctx := middleware.RequestContext(c)
	defer triggerSongInfoQueueSync(ctx, sc.store, orgID)

	data, found, errMsg := sc.songDetailData(ctx, orgID, songID)
	if !found {
		return c.Status(fiber.StatusNotFound).SendString(errMsg)
	}
	if mutationErr != "" {
		errMsg = appendErr(mutationErr, errMsg)
	}

	return render(c, views.SongDetailContent(data, errMsg))
}

// --- Roles ------------------------------------------------------------

func (sc *SchedulingController) AddSongDetailRole(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	ctx := middleware.RequestContext(c)
	songID := c.Params("song_id")

	var body struct {
		Role string `form:"role"`
	}
	errMsg := ""
	if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else if _, err := sc.store.CreateSongRole(ctx, orgID, scheduling.CreateSongRoleParams{SongID: songID, Role: body.Role}); err != nil {
		errMsg = err.Error()
	}

	return sc.renderSongDetail(c, songID, errMsg)
}

func (sc *SchedulingController) DeleteSongDetailRole(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	songID := c.Params("song_id")

	id, err := c.ParamsInt("id")
	errMsg := ""
	if err != nil {
		errMsg = "id must be an integer"
	} else if delErr := sc.store.DeleteSongRole(c.Context(), orgID, int64(id)); delErr != nil && !errors.Is(delErr, sql.ErrNoRows) {
		errMsg = delErr.Error()
	}

	return sc.renderSongDetail(c, songID, errMsg)
}

// --- Tags (labels) ------------------------------------------------------

func (sc *SchedulingController) AddSongDetailTag(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	ctx := middleware.RequestContext(c)
	songID := c.Params("song_id")

	var body struct {
		Category string `form:"category"`
		Value    string `form:"value"`
	}
	errMsg := ""
	if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else if _, err := sc.store.CreateSongTag(ctx, orgID, scheduling.CreateSongTagParams{
		SongID: songID, Category: body.Category, Value: body.Value,
	}); err != nil {
		errMsg = err.Error()
	}

	return sc.renderSongDetail(c, songID, errMsg)
}

func (sc *SchedulingController) DeleteSongDetailTag(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	songID := c.Params("song_id")

	id, err := c.ParamsInt("id")
	errMsg := ""
	if err != nil {
		errMsg = "id must be an integer"
	} else if delErr := sc.store.DeleteSongTag(c.Context(), orgID, int64(id)); delErr != nil && !errors.Is(delErr, sql.ErrNoRows) {
		errMsg = delErr.Error()
	}

	return sc.renderSongDetail(c, songID, errMsg)
}

// --- Contexts -------------------------------------------------------------

func (sc *SchedulingController) AddSongDetailContext(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	ctx := middleware.RequestContext(c)
	songID := c.Params("song_id")

	var body struct {
		ContextTypeID int64 `form:"context_type_id"`
	}
	errMsg := ""
	if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else if _, err := sc.store.CreateSongContext(ctx, orgID, scheduling.CreateSongContextParams{
		SongID: songID, ContextTypeID: body.ContextTypeID,
	}); err != nil {
		errMsg = err.Error()
	}

	return sc.renderSongDetail(c, songID, errMsg)
}

func (sc *SchedulingController) DeleteSongDetailContext(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	songID := c.Params("song_id")

	id, err := c.ParamsInt("id")
	errMsg := ""
	if err != nil {
		errMsg = "id must be an integer"
	} else if delErr := sc.store.DeleteSongContext(c.Context(), orgID, int64(id)); delErr != nil && !errors.Is(delErr, sql.ErrNoRows) {
		errMsg = delErr.Error()
	}

	return sc.renderSongDetail(c, songID, errMsg)
}

// --- Relationships (to other songs) ---------------------------------------

func (sc *SchedulingController) AddSongDetailRelationship(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	ctx := middleware.RequestContext(c)
	songID := c.Params("song_id")

	var body struct {
		OtherSongID      string `form:"other_song_id"`
		RelationshipType string `form:"relationship_type"`
		Notes            string `form:"notes"`
	}
	errMsg := ""
	if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else if _, err := sc.store.CreateSongRelationship(ctx, orgID, scheduling.CreateSongRelationshipParams{
		SongIDA: songID, SongIDB: body.OtherSongID, RelationshipType: body.RelationshipType, Notes: body.Notes,
	}); err != nil {
		errMsg = err.Error()
	}

	return sc.renderSongDetail(c, songID, errMsg)
}

func (sc *SchedulingController) DeleteSongDetailRelationship(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	songID := c.Params("song_id")

	id, err := c.ParamsInt("id")
	errMsg := ""
	if err != nil {
		errMsg = "id must be an integer"
	} else if delErr := sc.store.DeleteSongRelationship(c.Context(), orgID, int64(id)); delErr != nil && !errors.Is(delErr, sql.ErrNoRows) {
		errMsg = delErr.Error()
	}

	return sc.renderSongDetail(c, songID, errMsg)
}

// --- Preferences (people preferred/discouraged for this song) ------------

func (sc *SchedulingController) AddSongDetailPreference(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	ctx := middleware.RequestContext(c)
	songID := c.Params("song_id")

	var body struct {
		PersonID   string `form:"person_id"`
		Preference string `form:"preference"`
		Notes      string `form:"notes"`
	}
	errMsg := ""
	if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else if _, err := sc.store.SetSongPersonPreference(ctx, orgID, scheduling.SetSongPersonPreferenceParams{
		SongID: songID, PersonID: body.PersonID, Preference: body.Preference, Notes: body.Notes,
	}); err != nil {
		errMsg = err.Error()
	}

	return sc.renderSongDetail(c, songID, errMsg)
}

func (sc *SchedulingController) DeleteSongDetailPreference(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	songID := c.Params("song_id")

	id, err := c.ParamsInt("id")
	errMsg := ""
	if err != nil {
		errMsg = "id must be an integer"
	} else if delErr := sc.store.DeleteSongPersonPreference(c.Context(), orgID, int64(id)); delErr != nil && !errors.Is(delErr, sql.ErrNoRows) {
		errMsg = delErr.Error()
	}

	return sc.renderSongDetail(c, songID, errMsg)
}

// --- Vocal range ------------------------------------------------------

func (sc *SchedulingController) SetSongDetailVocalRange(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	ctx := middleware.RequestContext(c)
	songID := c.Params("song_id")

	var body struct {
		ReferenceKey string `form:"reference_key"`
		Lowest       string `form:"lowest_note"`
		Highest      string `form:"highest_note"`
	}
	errMsg := ""
	if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else {
		errMsg = sc.setSongVocalRangeFromForm(ctx, orgID, songID, body.ReferenceKey, body.Lowest, body.Highest)
	}

	return sc.renderSongDetail(c, songID, errMsg)
}

func (sc *SchedulingController) DeleteSongDetailVocalRange(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	ctx := middleware.RequestContext(c)
	songID := c.Params("song_id")

	id, err := c.ParamsInt("id")
	errMsg := ""
	if err != nil {
		errMsg = "id must be an integer"
	} else if delErr := sc.store.DeleteSongVocalRange(ctx, orgID, int64(id)); delErr != nil && !errors.Is(delErr, sql.ErrNoRows) {
		errMsg = delErr.Error()
	}

	return sc.renderSongDetail(c, songID, errMsg)
}

// --- Transition notes (debrief / talking points) --------------------------
//
// Thin equivalents of QuickAddTransitionNote/DeleteTransitionNote
// (transition_notes.go), calling the exact same store methods - the only
// difference is what gets re-rendered at the end (this song's own detail
// page fragment, not the Build page's SetBuilderBody) and that there's no
// service_type_id/plan_id in the URL to thread through, since this page
// isn't plan-scoped at all. See that file's own doc comment on
// CreateTransitionNoteParams' SongIDA/SongIDB/direction convention, mirrored
// here exactly.

func (sc *SchedulingController) AddSongDetailTransitionNote(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	ctx := middleware.RequestContext(c)
	songID := c.Params("song_id")

	var body struct {
		OtherSongID string `form:"other_song_id"`
		Direction   string `form:"direction"`
		Kind        string `form:"kind"`
		Note        string `form:"note"`
	}
	errMsg := ""
	if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else {
		params := scheduling.CreateTransitionNoteParams{Kind: body.Kind, Note: body.Note}
		switch {
		case body.OtherSongID == "":
			params.SongIDA = songID
		case body.Direction == "from":
			params.SongIDA = body.OtherSongID
			params.SongIDB = songID
		default:
			params.SongIDA = songID
			params.SongIDB = body.OtherSongID
		}
		if _, err := sc.store.CreateTransitionNote(ctx, orgID, params); err != nil {
			errMsg = err.Error()
		}
	}

	return sc.renderSongDetail(c, songID, errMsg)
}

func (sc *SchedulingController) DeleteSongDetailTransitionNote(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	songID := c.Params("song_id")

	id, err := c.ParamsInt("id")
	errMsg := ""
	if err != nil {
		errMsg = "id must be an integer"
	} else if delErr := sc.store.DeleteTransitionNote(c.Context(), orgID, int64(id)); delErr != nil && !errors.Is(delErr, sql.ErrNoRows) {
		errMsg = delErr.Error()
	}

	return sc.renderSongDetail(c, songID, errMsg)
}
