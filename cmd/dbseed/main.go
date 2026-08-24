// Command dbseed populates the local database with a small set of example
// scheduling data (song relationships, roles, tags, preferences, person
// relationships) - enough to look at every setup page and the set builder
// without having to click through the UI by hand first.
//
// Run it with:
//
//	go run ./cmd/dbseed
//
// Songs and people themselves aren't stored in this database (see
// internal/scheduling's package doc) - they live in Planning Center, so
// seeded rows have to reference *some* song/person ID. If you've signed
// into the app in your browser at least once, dbseed borrows that session's
// PCO access token and seeds against your org's real songs and people,
// falling back to obviously-fake placeholder IDs ("seed-song-1" etc.) for
// whatever it couldn't find - a brand new PCO org with only one or two
// songs catalogued still gets real IDs for those, not an all-or-nothing
// switch to fully-fake data.
//
// Every lookup table (song tag categories, song/person relationship types,
// song roles, song preference types) is read live rather than hardcoded, so
// a type added to one of those tables later (a migration, or by hand) gets
// an example seeded here automatically, no code change needed - the same
// "no code change needed" property those tables already give the rest of
// the app.
//
// Safe to re-run: every table it seeds either upserts (song roles/tags,
// song/person preferences - so re-seeding always reports success, even
// when nothing actually changed) or quietly skips a row that's already
// there (song/person relationships - which do report "already seeded" on a
// repeat run), so running it again doesn't duplicate data. See the summary
// line at the end for what actually happened.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"pco-assistant/internal/auth"
	"pco-assistant/internal/database"
	"pco-assistant/internal/scheduling"

	pco "github.com/drkgrntt/go-pco"
	_ "github.com/joho/godotenv/autoload"
)

// seedSongCount/seedPersonCount are how many songs/people dbseed wants to
// work with - enough that every song role (six as of migration 00020) and
// every relationship type can get its own distinct example. Not a hard
// requirement: seedSubjects uses as many real ones as your PCO org
// actually has and pads the rest with placeholders, so a smaller org still
// gets a partially-real seed instead of an all-placeholder one.
const (
	seedSongCount   = 6
	seedPersonCount = 4
)

type seedSong struct{ ID, Title string }
type seedPerson struct{ ID, Name string }

func main() {
	if os.Getenv("APP_ENV") != "local" {
		log.Fatal("dbseed refuses to run unless APP_ENV=local - .env should never point this at anything but a disposable local database")
	}

	db, err := database.Open()
	if err != nil {
		log.Fatalf("connect to database: %v", err)
	}
	defer db.Close()

	// Defensive, not the normal path: makes dbseed work even against a
	// database dbreset (or a fresh install) just emptied, without requiring
	// `go run ./cmd/api` to have been started first.
	if err := database.Migrate(db); err != nil {
		log.Fatalf("run migrations: %v", err)
	}

	ctx := context.Background()
	orgID, songs, people, realSongs, realPeople := seedSubjects(ctx, db)
	announceSubjects(songs, people, realSongs, realPeople)

	store := scheduling.NewStore(db)
	stats := &seedStats{}
	seedSongTags(ctx, store, stats, orgID, songs)
	seedSongRoles(ctx, store, stats, orgID, songs)
	seedSongRelationships(ctx, store, stats, orgID, songs)
	seedSongPreferences(ctx, store, stats, orgID, songs, people)
	seedPersonRelationships(ctx, store, stats, orgID, people)

	stats.summarize()
}

// placeholderOrgID stands in for a real PCO organization id when there's no
// usable signed-in session to borrow one from (see seedSubjects) - every
// scheduling.Store method now requires an organization id (see migration
// 00019), so seeded rows need *some* value even in the placeholder case.
const placeholderOrgID = "seed-org"

// seedSubjects gathers up to seedSongCount songs and seedPersonCount people
// from the signed-in PCO org (see MostRecentSession's doc comment for why
// it never refreshes that session's token) - using every real one it can
// get and padding whatever's left with synthetic placeholder IDs, rather
// than discarding real data just because the org doesn't have enough of
// it. realSongCount/realPersonCount report how many of the returned slices
// are real (always a prefix of each slice), so callers can explain the mix
// without re-deriving it. The returned organization id is that session's
// real PCO org id whenever one was found, even if it didn't have enough
// songs/people to avoid placeholders entirely - a real org id still keeps
// every seeded row properly org-scoped.
func seedSubjects(ctx context.Context, db *sql.DB) (organizationID string, songs []seedSong, people []seedPerson, realSongCount, realPersonCount int) {
	reqCtx := ctx
	organizationID = placeholderOrgID
	if session, err := auth.NewStore(db).MostRecentSession(ctx); err == nil {
		// A near-expired token is left alone rather than refreshed here -
		// see MostRecentSession - so a stale one just falls through to the
		// plain ctx below (and, from there, to an all-placeholder result).
		if time.Now().Add(5 * time.Minute).Before(session.TokenExpiresAt) {
			reqCtx = pco.WithAccessToken(ctx, session.AccessToken)
			organizationID = session.OrganizationID
		}
	}

	if resp, err := pco.GetSongs(reqCtx, &pco.SongsParams{OrderBy: "title", PerPage: seedSongCount}); err == nil {
		for _, s := range resp.Data {
			songs = append(songs, seedSong{ID: s.ID, Title: s.Attributes.Title})
		}
	}
	realSongCount = len(songs)
	for i := len(songs); i < seedSongCount; i++ {
		songs = append(songs, placeholderSong(i))
	}

	if resp, err := pco.GetPeople(reqCtx, &pco.PeopleParams{OrderBy: "last_name", PerPage: seedPersonCount}); err == nil {
		for _, p := range resp.Data {
			people = append(people, seedPerson{ID: p.ID, Name: p.Attributes.Name})
		}
	}
	realPersonCount = len(people)
	for i := len(people); i < seedPersonCount; i++ {
		people = append(people, placeholderPerson(i))
	}

	return organizationID, songs, people, realSongCount, realPersonCount
}

func placeholderSong(i int) seedSong {
	id := fmt.Sprintf("seed-song-%d", i+1)
	return seedSong{ID: id, Title: id + " (placeholder)"}
}

func placeholderPerson(i int) seedPerson {
	id := fmt.Sprintf("seed-person-%d", i+1)
	return seedPerson{ID: id, Name: id + " (placeholder)"}
}

// announceSubjects explains, once, up front, exactly what dbseed is about
// to seed against - real PCO data, an all-placeholder fallback, or (a
// smaller org, or a partially-expired session that still resolved an org
// id) a mix of both - so it's obvious from the first two lines of output
// which case a given run landed in, rather than having to infer it from
// scanning for "seed-song-" in the rows below.
func announceSubjects(songs []seedSong, people []seedPerson, realSongs, realPeople int) {
	switch {
	case realSongs == len(songs) && realPeople == len(people):
		fmt.Printf("Signed-in PCO session found - seeding against %d real song(s) and %d real person/people.\n", realSongs, realPeople)
	case realSongs == 0 && realPeople == 0:
		fmt.Println("No usable signed-in PCO session found (or its token has expired) - seeding with placeholder song/person IDs instead.")
		fmt.Println("Sign in once at http://localhost:8080/login, then re-run `go run ./cmd/dbseed` to seed against your real Planning Center songs and people instead.")
	default:
		fmt.Printf(
			"Signed-in PCO session found, but your org only has %d song(s) and %d person/people to work with - using those and padding the rest with placeholders (wanted %d songs / %d people for full variety).\n",
			realSongs, realPeople, len(songs), len(people),
		)
	}
	fmt.Println()
}

// seedStats tallies what happened across every seeded row, printed as one
// summary line at the end (see summarize) - the direct fix for "it didn't
// even seem to seed anything": a re-run's mix of freshly-(re)applied rows
// and already-there rows is easy to misread as "nothing happened" without
// an explicit count to look at instead of scanning +/= lines by eye.
type seedStats struct {
	// applied counts every row report saw no error for. Note this isn't
	// strictly "brand new": CreateSongTag/CreateSongRole/
	// SetSongPersonPreference all upsert (ON CONFLICT DO UPDATE - see their
	// own doc comments), so a repeat run re-applies and reports success for
	// those every time, whether or not the row actually changed. Only
	// CreateSongRelationship/CreatePersonRelationship (ON CONFLICT DO
	// NOTHING) distinguish a genuine repeat via existed below.
	applied int
	// existed counts a row report's "already exists" error - only ever
	// reported by the two DO-NOTHING relationship tables, see applied.
	existed int
	failed  int
}

func (s *seedStats) report(action string, err error) {
	switch {
	case err == nil:
		fmt.Println("  +", action)
		s.applied++
	case strings.Contains(err.Error(), "already exists"):
		fmt.Println("  =", action, "(already seeded)")
		s.existed++
	default:
		log.Printf("  ! %s: %v", action, err)
		s.failed++
	}
}

func (s *seedStats) summarize() {
	fmt.Println()
	switch {
	case s.failed > 0:
		fmt.Printf("Seeding finished with problems: %d applied, %d already existed, %d failed - see the ! lines above.\n", s.applied, s.existed, s.failed)
	case s.applied == 0 && s.existed > 0:
		fmt.Printf("Nothing to apply - all %d relationship row(s) already existed, and nothing else needed re-applying. This is expected on a re-run.\n", s.existed)
	default:
		fmt.Printf("Seeding complete: %d row(s) applied, %d already existed.\n", s.applied, s.existed)
		if s.existed == 0 {
			fmt.Println("(Song tags/roles/preferences always show as applied, even on a re-run, since they upsert in place - only relationships report \"already seeded\".)")
		}
	}
}

// seedSongTags tags a song with an example value for every category
// song_tag_categories actually has (see ListSongTagCategories) - a
// category added to that table later gets seeded here automatically. Songs
// are picked by cycling through the available set, so seedSongCount only
// needs to be as large as you want the variety to be, not exactly equal to
// the category count.
func seedSongTags(ctx context.Context, store *scheduling.Store, stats *seedStats, organizationID string, songs []seedSong) {
	fmt.Println("Song tags:")
	categories, err := store.ListSongTagCategories(ctx)
	if err != nil {
		log.Printf("  ! loading song tag categories: %v", err)
		return
	}

	// Nicer example values for the categories this seeder knows about
	// today - a category it doesn't recognize still gets a serviceable
	// generic example instead of being skipped.
	sampleValues := map[string]string{
		scheduling.TagCategoryTheme:     "Grace",
		scheduling.TagCategoryScripture: "Romans 8:1",
		scheduling.TagCategoryIdea:      "Good for baptisms",
	}

	for i, cat := range categories {
		song := songs[i%len(songs)]
		value := sampleValues[cat.Code]
		if value == "" {
			value = cat.Label + " example"
		}

		_, err := store.CreateSongTag(ctx, organizationID, scheduling.CreateSongTagParams{SongID: song.ID, Category: cat.Code, Value: value})
		stats.report(fmt.Sprintf("%s: %q on %s", cat.Code, value, song.Title), err)
	}
}

// seedSongRoles assigns every role song_role_types actually has (see
// ListSongRoleTypes - opener/response plus, as of migration 00020,
// communion/prayer/reflection/baptism) to a song, one each, cycling
// through the available songs the same way seedSongTags does.
func seedSongRoles(ctx context.Context, store *scheduling.Store, stats *seedStats, organizationID string, songs []seedSong) {
	fmt.Println("Song roles:")
	types, err := store.ListSongRoleTypes(ctx)
	if err != nil {
		log.Printf("  ! loading song role types: %v", err)
		return
	}

	for i, t := range types {
		song := songs[i%len(songs)]
		_, err := store.CreateSongRole(ctx, organizationID, scheduling.CreateSongRoleParams{SongID: song.ID, Role: t.Code})
		stats.report(fmt.Sprintf("%s: %s", t.Code, song.Title), err)
	}
}

// seedSongRelationships relates a pair of songs for every relationship
// type song_relationship_types actually has (see ListSongRelationshipTypes)
// - pairs cycle through the available songs two at a time, so a small
// library reuses the same pair for every type (still valid - relationship
// rows are unique per type, not per pair) rather than failing outright.
func seedSongRelationships(ctx context.Context, store *scheduling.Store, stats *seedStats, organizationID string, songs []seedSong) {
	fmt.Println("Song relationships:")
	if len(songs) < 2 {
		fmt.Println("  (skipped - need at least 2 songs)")
		return
	}

	types, err := store.ListSongRelationshipTypes(ctx)
	if err != nil {
		log.Printf("  ! loading song relationship types: %v", err)
		return
	}

	for i, t := range types {
		a := songs[(i*2)%len(songs)]
		b := songs[(i*2+1)%len(songs)]
		_, err := store.CreateSongRelationship(ctx, organizationID, scheduling.CreateSongRelationshipParams{SongIDA: a.ID, SongIDB: b.ID, RelationshipType: t.Code})
		stats.report(fmt.Sprintf("%s: %s <-> %s", t.Code, a.Title, b.Title), err)
	}
}

// seedSongPreferences sets a song/person preference for every preference
// type song_preference_types actually has (see ListSongPreferenceTypes),
// cycling through the available songs and people the same way the other
// seed functions do.
func seedSongPreferences(ctx context.Context, store *scheduling.Store, stats *seedStats, organizationID string, songs []seedSong, people []seedPerson) {
	fmt.Println("Song/person preferences:")
	types, err := store.ListSongPreferenceTypes(ctx)
	if err != nil {
		log.Printf("  ! loading song preference types: %v", err)
		return
	}

	notes := map[string]string{
		scheduling.PreferencePreferred:   "Great vocal fit.",
		scheduling.PreferenceDiscouraged: "Key doesn't suit their range.",
	}

	for i, t := range types {
		song := songs[i%len(songs)]
		person := people[i%len(people)]
		note := notes[t.Code]
		if note == "" {
			note = t.Label + " example"
		}

		_, err := store.SetSongPersonPreference(ctx, organizationID, scheduling.SetSongPersonPreferenceParams{
			SongID: song.ID, PersonID: person.ID, Preference: t.Code, Notes: note,
		})
		stats.report(fmt.Sprintf("%s: %s / %s", t.Code, song.Title, person.Name), err)
	}
}

// seedPersonRelationships relates a pair of people for every relationship
// type person_relationship_types actually has (see
// ListPersonRelationshipTypes) - see seedSongRelationships for the pairing/
// small-library reasoning this mirrors. A directional type's pair order
// (e.g. mentorship's "A mentors B") is preserved for whatever the cycled
// pair happens to be.
func seedPersonRelationships(ctx context.Context, store *scheduling.Store, stats *seedStats, organizationID string, people []seedPerson) {
	fmt.Println("Person relationships:")
	if len(people) < 2 {
		fmt.Println("  (skipped - need at least 2 people)")
		return
	}

	types, err := store.ListPersonRelationshipTypes(ctx)
	if err != nil {
		log.Printf("  ! loading person relationship types: %v", err)
		return
	}

	for i, t := range types {
		a := people[(i*2)%len(people)]
		b := people[(i*2+1)%len(people)]
		_, err := store.CreatePersonRelationship(ctx, organizationID, scheduling.CreatePersonRelationshipParams{PersonIDA: a.ID, PersonIDB: b.ID, RelationshipType: t.Code})
		stats.report(fmt.Sprintf("%s: %s <-> %s", t.Code, a.Name, b.Name), err)
	}
}
