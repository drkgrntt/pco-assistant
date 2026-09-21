package controllers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	"pco-assistant/internal/scheduling"
	"pco-assistant/internal/server/middleware"
	views "pco-assistant/internal/views/scheduling"

	"github.com/gofiber/fiber/v2"
)

// Phase 2 of docs/plans/in-progress/song-person-detail-pages-plan.md: a
// standalone People library index (/scheduling/people) plus a per-person
// detail page (/scheduling/people/:person_id) aggregating what used to be
// spread across the Person Relationships and Vocal Range setup pages, plus
// the "Person" half of Song/Person Preferences - see that plan's "Person
// side - almost none of it is library-level" section for why this is a
// much smaller page than the song detail page's own aggregate: no
// assignment status, conflict warnings, or team/position info, since none
// of that has meaning outside a specific plan occurrence. Purely additive
// - the flat setup pages this subsumes (PersonRelationshipsPage,
// PersonVocalRangePage, the "Person" side of PreferencesPage) stay exactly
// as they are; retiring them is Phase 3, not this one.

// localPersonIDs returns every distinct person id this app already has at
// least one local record for within organizationID - the union of
// scheduling.Store.DistinctPersonRelationshipPersonIDs/
// DistinctSongPersonPreferencePersonIDs/DistinctPersonVocalRangePersonIDs
// (three cheap local-DB queries, no PCO call). This is the People library
// index's whole reason for existing without ever paging through PCO's
// full People directory - see the plan's "songs are a cached library,
// people are not" section for why that would be an unbounded, previously-
// unpaid PCO cost this app has never taken on anywhere else.
func localPersonIDs(ctx context.Context, store *scheduling.Store, organizationID string) ([]string, error) {
	relationshipIDs, err := store.DistinctPersonRelationshipPersonIDs(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	preferenceIDs, err := store.DistinctSongPersonPreferencePersonIDs(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	vocalRangeIDs, err := store.DistinctPersonVocalRangePersonIDs(ctx, organizationID)
	if err != nil {
		return nil, err
	}

	return dedupeIDs(relationshipIDs, preferenceIDs, vocalRangeIDs), nil
}

// dedupeIDs merges any number of id slices into one distinct, sorted slice -
// split out of localPersonIDs as its own pure function so the merge/dedup
// logic is unit-testable without a database (see person_library_test.go).
func dedupeIDs(groups ...[]string) []string {
	seen := make(map[string]bool)
	var ids []string
	for _, group := range groups {
		for _, id := range group {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	sort.Strings(ids)
	return ids
}

// personLibraryRows resolves ids to display rows via lookups
// (personLookupMapForIDs) and sorts them by display name (case-insensitive)
// for the index page - an id missing from lookups (a batched PCO lookup
// failure, e.g. a since-deleted person) falls back to showing the raw id,
// same convention as every other name lookup in this package.
func personLibraryRows(ids []string, lookups map[string]personLookup) []views.PersonLibraryRow {
	rows := make([]views.PersonLibraryRow, len(ids))
	for i, id := range ids {
		l, ok := lookups[id]
		name := id
		if ok && l.Name != "" {
			name = l.Name
		}
		rows[i] = views.PersonLibraryRow{PersonID: id, Name: name, AvatarURL: l.AvatarURL}
	}
	sort.Slice(rows, func(i, j int) bool {
		return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name)
	})
	return rows
}

// PersonLibraryPage renders the People library index - see localPersonIDs
// for how its person set is derived without ever paging PCO's full People
// directory.
func (sc *SchedulingController) PersonLibraryPage(c *fiber.Ctx) error {
	_, personName := middleware.CurrentPerson(c)
	orgName := middleware.CurrentOrganization(c)
	orgID := middleware.CurrentOrganizationID(c)
	ctx := middleware.RequestContext(c)

	ids, err := localPersonIDs(c.Context(), sc.store, orgID)
	if err != nil {
		return render(c, views.PersonLibraryIndex(personName, orgName, nil, err.Error()))
	}

	lookups := personLookupMapForIDs(ctx, ids)
	return render(c, views.PersonLibraryIndex(personName, orgName, personLibraryRows(ids, lookups), ""))
}

// personDetailVocalRangeView builds PersonDetailVocalRangeSection's view
// model for personID from their single stored range row (or its absence) -
// Line reuses personVocalRangeLine's own markup (person_detail_card.templ,
// see PersonDetailVocalRangeView's doc comment) rather than duplicating its
// display+play-button logic; LowestDisplay/HighestDisplay pre-fill the
// add/edit form with whatever's currently on file.
func personDetailVocalRangeView(vr scheduling.PersonVocalRange, hasRange bool) views.PersonDetailVocalRangeView {
	if !hasRange {
		return views.PersonDetailVocalRangeView{}
	}

	return views.PersonDetailVocalRangeView{
		ID:  vr.ID,
		Has: true,
		Line: views.AssignmentView{
			VocalRangeDisplay:          fmt.Sprintf("%s-%s", formatVocalNote(vr.LowestPitchClass, vr.LowestOctave), formatVocalNote(vr.HighestPitchClass, vr.HighestOctave)),
			VocalRangeHasPlayableNotes: true,
			VocalRangeLowPitchClass:    vr.LowestPitchClass,
			VocalRangeLowOctave:        vr.LowestOctave,
			VocalRangeHighPitchClass:   vr.HighestPitchClass,
			VocalRangeHighOctave:       vr.HighestOctave,
		},
		LowestDisplay:  formatVocalNote(vr.LowestPitchClass, vr.LowestOctave),
		HighestDisplay: formatVocalNote(vr.HighestPitchClass, vr.HighestOctave),
	}
}

// PersonDetailPage renders one person's aggregate detail page - see this
// file's own package doc comment for what it deliberately does and doesn't
// show.
func (sc *SchedulingController) PersonDetailPage(c *fiber.Ctx) error {
	_, personName := middleware.CurrentPerson(c)
	orgName := middleware.CurrentOrganization(c)
	orgID := middleware.CurrentOrganizationID(c)
	personID := c.Params("person_id")
	ctx := middleware.RequestContext(c)

	lookup := personLookupMapForIDs(ctx, []string{personID})[personID]
	displayName := lookup.Name
	if displayName == "" {
		displayName = personID
	}

	relationships, relErr := sc.store.ListPersonRelationships(c.Context(), orgID, personID)
	relationshipTypes, rtErr := sc.store.ListPersonRelationshipTypes(c.Context())
	relationshipNames := personNameMapForIDs(ctx, relationshipPersonIDs(relationships))
	relationshipsErrMsg := ""
	if relErr != nil {
		relationshipsErrMsg = relErr.Error()
	} else if rtErr != nil {
		relationshipsErrMsg = rtErr.Error()
	}

	vr, vrErr := sc.store.PersonVocalRange(c.Context(), orgID, personID)
	hasVocalRange := vrErr == nil
	vocalRangeErrMsg := ""
	if vrErr != nil && !errors.Is(vrErr, sql.ErrNoRows) {
		vocalRangeErrMsg = vrErr.Error()
	}

	preferences, prefErr := sc.store.ListSongPersonPreferences(c.Context(), orgID, "", personID)
	preferenceTypes, ptErr := sc.store.ListSongPreferenceTypes(c.Context())
	songs, songsErr := fetchActiveSongs(ctx, orgID)
	preferencesErrMsg := ""
	if prefErr != nil {
		preferencesErrMsg = prefErr.Error()
	} else if ptErr != nil {
		preferencesErrMsg = ptErr.Error()
	} else if songsErr != nil {
		preferencesErrMsg = "loading songs from Planning Center: " + songsErr.Error()
	}

	data := views.PersonDetailView{
		PersonID:  personID,
		Name:      displayName,
		AvatarURL: lookup.AvatarURL,

		Relationships:          relationships,
		RelationshipTypes:      relationshipTypes,
		PersonNames:            relationshipNames,
		RelationshipTypeLabels: personRelationshipTypeLabelMap(relationshipTypes),
		RelationshipsErr:       relationshipsErrMsg,

		VocalRange:    personDetailVocalRangeView(vr, hasVocalRange),
		VocalRangeErr: vocalRangeErrMsg,

		Preferences:      preferences,
		PreferenceTypes:  preferenceTypes,
		Songs:            songs,
		SongTitles:       songTitleMap(songs),
		PreferenceLabels: songPreferenceTypeLabelMap(preferenceTypes),
		PreferencesErr:   preferencesErrMsg,
	}

	return render(c, views.PersonDetailPage(personName, orgName, data))
}

// renderPersonDetailRelationshipsSection re-fetches personID's relationships
// and re-renders just that section - the shared tail of
// AddPersonDetailRelationship/DeletePersonDetailRelationship, mirroring
// CreateRelationship/DeleteRelationship's own re-fetch-and-render pattern
// (scheduling.go).
func (sc *SchedulingController) renderPersonDetailRelationshipsSection(c *fiber.Ctx, orgID, personID, errMsg string) error {
	relationships, err := sc.store.ListPersonRelationships(c.Context(), orgID, personID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}

	relationshipTypes, _ := sc.store.ListPersonRelationshipTypes(c.Context())
	names := personNameMapForIDs(middleware.RequestContext(c), relationshipPersonIDs(relationships))

	return render(c, views.PersonDetailRelationshipsSection(personID, relationships, relationshipTypes, names, personRelationshipTypeLabelMap(relationshipTypes), errMsg))
}

// AddPersonDetailRelationship adds a relationship between personID (the
// page this form lives on) and whichever person the "Relate to" picker
// selected - the person-detail-page counterpart to CreatePersonRelationship
// (people.go), scoped to one person instead of taking both sides from the
// form.
func (sc *SchedulingController) AddPersonDetailRelationship(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	personID := c.Params("person_id")

	var body struct {
		OtherPersonID    string `form:"other_person_id"`
		RelationshipType string `form:"relationship_type"`
		Notes            string `form:"notes"`
	}
	errMsg := ""
	if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else if body.OtherPersonID == "" {
		errMsg = "person is required"
	} else if _, err := sc.store.CreatePersonRelationship(c.Context(), orgID, scheduling.CreatePersonRelationshipParams{
		PersonIDA:        personID,
		PersonIDB:        body.OtherPersonID,
		RelationshipType: body.RelationshipType,
		Notes:            body.Notes,
	}); err != nil {
		errMsg = err.Error()
	}

	return sc.renderPersonDetailRelationshipsSection(c, orgID, personID, errMsg)
}

// DeletePersonDetailRelationship deletes one relationship row and
// re-renders personID's relationships section.
func (sc *SchedulingController) DeletePersonDetailRelationship(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	personID := c.Params("person_id")

	id, err := c.ParamsInt("id")
	errMsg := ""
	if err != nil {
		errMsg = "id must be an integer"
	} else if delErr := sc.store.DeletePersonRelationship(c.Context(), orgID, int64(id)); delErr != nil && !errors.Is(delErr, sql.ErrNoRows) {
		errMsg = delErr.Error()
	}

	return sc.renderPersonDetailRelationshipsSection(c, orgID, personID, errMsg)
}

// renderPersonDetailVocalRangeSection re-fetches personID's vocal range and
// re-renders just that section - shared tail of
// SetPersonDetailVocalRange/DeletePersonDetailVocalRange.
func (sc *SchedulingController) renderPersonDetailVocalRangeSection(c *fiber.Ctx, orgID, personID, errMsg string) error {
	vr, vrErr := sc.store.PersonVocalRange(c.Context(), orgID, personID)
	hasRange := vrErr == nil
	if vrErr != nil && !errors.Is(vrErr, sql.ErrNoRows) {
		errMsg = appendErr(errMsg, vrErr.Error())
	}

	return render(c, views.PersonDetailVocalRangeSection(personID, personDetailVocalRangeView(vr, hasRange), errMsg))
}

// SetPersonDetailVocalRange creates or replaces personID's stored vocal
// range - same parsing/validation as SetPersonVocalRange (vocal_range.go),
// just scoped to this page's one person instead of a picker-selected one.
func (sc *SchedulingController) SetPersonDetailVocalRange(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	personID := c.Params("person_id")

	var body struct {
		Lowest  string `form:"lowest_note"`
		Highest string `form:"highest_note"`
	}
	errMsg := ""
	if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else if lowPC, lowOct, ok := parseVocalNote(body.Lowest); !ok {
		errMsg = fmt.Sprintf("%q isn't a note - try something like G3", body.Lowest)
	} else if highPC, highOct, ok := parseVocalNote(body.Highest); !ok {
		errMsg = fmt.Sprintf("%q isn't a note - try something like D5", body.Highest)
	} else if absoluteVocalSemitone(lowPC, lowOct) > absoluteVocalSemitone(highPC, highOct) {
		errMsg = "lowest note must be at or below the highest note"
	} else if _, err := sc.store.SetPersonVocalRange(c.Context(), orgID, scheduling.SetPersonVocalRangeParams{
		PersonID:          personID,
		LowestPitchClass:  lowPC,
		LowestOctave:      lowOct,
		HighestPitchClass: highPC,
		HighestOctave:     highOct,
	}); err != nil {
		errMsg = err.Error()
	}

	return sc.renderPersonDetailVocalRangeSection(c, orgID, personID, errMsg)
}

// DeletePersonDetailVocalRange deletes personID's stored vocal range row
// and re-renders that section.
func (sc *SchedulingController) DeletePersonDetailVocalRange(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	personID := c.Params("person_id")

	id, err := c.ParamsInt("id")
	errMsg := ""
	if err != nil {
		errMsg = "id must be an integer"
	} else if delErr := sc.store.DeletePersonVocalRange(c.Context(), orgID, int64(id)); delErr != nil && !errors.Is(delErr, sql.ErrNoRows) {
		errMsg = delErr.Error()
	}

	return sc.renderPersonDetailVocalRangeSection(c, orgID, personID, errMsg)
}

// renderPersonDetailPreferencesSection re-fetches personID's song
// preferences and re-renders just that section - shared tail of
// AddPersonDetailPreference/DeletePersonDetailPreference.
func (sc *SchedulingController) renderPersonDetailPreferencesSection(c *fiber.Ctx, orgID, personID, errMsg string) error {
	ctx := middleware.RequestContext(c)

	preferences, err := sc.store.ListSongPersonPreferences(c.Context(), orgID, "", personID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
	}

	preferenceTypes, _ := sc.store.ListSongPreferenceTypes(c.Context())
	songs, _ := fetchActiveSongs(ctx, orgID)

	return render(c, views.PersonDetailPreferencesSection(personID, preferences, preferenceTypes, songs, songTitleMap(songs), songPreferenceTypeLabelMap(preferenceTypes), errMsg))
}

// AddPersonDetailPreference sets (creates or, on a repeat submit for the
// same song, upserts - see SetSongPersonPreference's ON CONFLICT clause) a
// song preference for personID - the person-detail-page counterpart to
// SetPreference (scheduling.go), scoped to this page's one person, with the
// song side picked from a plain <select> of the active song library rather
// than a picker (see PersonDetailPreferencesSection's own doc comment on
// why - no SongPickerField exists anywhere in this codebase).
func (sc *SchedulingController) AddPersonDetailPreference(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	personID := c.Params("person_id")

	var body struct {
		SongID     string `form:"song_id"`
		Preference string `form:"preference"`
		Notes      string `form:"notes"`
	}
	errMsg := ""
	if err := c.BodyParser(&body); err != nil {
		errMsg = err.Error()
	} else if body.SongID == "" {
		errMsg = "song is required"
	} else if _, err := sc.store.SetSongPersonPreference(c.Context(), orgID, scheduling.SetSongPersonPreferenceParams{
		SongID:     body.SongID,
		PersonID:   personID,
		Preference: body.Preference,
		Notes:      body.Notes,
	}); err != nil {
		errMsg = err.Error()
	}

	return sc.renderPersonDetailPreferencesSection(c, orgID, personID, errMsg)
}

// DeletePersonDetailPreference deletes one song preference row and
// re-renders personID's preferences section.
func (sc *SchedulingController) DeletePersonDetailPreference(c *fiber.Ctx) error {
	orgID := middleware.CurrentOrganizationID(c)
	personID := c.Params("person_id")

	id, err := c.ParamsInt("id")
	errMsg := ""
	if err != nil {
		errMsg = "id must be an integer"
	} else if delErr := sc.store.DeleteSongPersonPreference(c.Context(), orgID, int64(id)); delErr != nil && !errors.Is(delErr, sql.ErrNoRows) {
		errMsg = delErr.Error()
	}

	return sc.renderPersonDetailPreferencesSection(c, orgID, personID, errMsg)
}
