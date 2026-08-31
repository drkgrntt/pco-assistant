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
// PCO access token: songs are actually created in Planning Center (a fixed
// set of canned example songs - see cannedSongs in songs.go - skipping any
// that already exist by title, so a repeat run doesn't duplicate the
// library), each with its auto-created default arrangement updated to
// carry a real key/tempo, the same "key, capo, arrangement" setup Planning
// Center's own "add song" flow leads you through by hand (minus capo
// itself, which has no API field to set at all). People are read, not
// created, from whichever real people your org already has. With no usable
// session, both songs and people fall back to obviously-fake placeholder
// IDs ("seed-song-1" etc.) instead.
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
	"os"
	"strings"
	"time"

	"pco-assistant/internal/auth"
	"pco-assistant/internal/database"
	"pco-assistant/internal/logger"
	"pco-assistant/internal/scheduling"

	pco "github.com/drkgrntt/go-pco"
	_ "github.com/joho/godotenv/autoload"
)

// seedPersonCount is how many people dbseed wants to work with - enough
// that every seed function below can give each lookup type more than one
// example (see each function's own multiplier) without constantly reusing
// the same one or two people, so the result reads like an actual
// planned-out setup rather than a bare minimum smoke test. Not a hard
// requirement: seedSubjects uses as many real people as your PCO org
// actually has and pads the rest with placeholders, so a smaller org still
// gets a partially-real seed instead of an all-placeholder one. Songs have
// no equivalent constant - dbseed creates exactly len(cannedSongs) of them
// (see songs.go), rather than sampling however many the org happens to
// have.
const seedPersonCount = 6

type seedSong struct{ ID, Title string }
type seedPerson struct{ ID, Name string }

func main() {
	if os.Getenv("APP_ENV") != "local" {
		logger.Fatal("dbseed refuses to run unless APP_ENV=local - .env should never point this at anything but a disposable local database")
	}

	db, err := database.Open()
	if err != nil {
		logger.Fatal("connect to database", "error", err)
	}
	defer db.Close()

	// Defensive, not the normal path: makes dbseed work even against a
	// database dbreset (or a fresh install) just emptied, without requiring
	// `go run ./cmd/api` to have been started first.
	if err := database.Migrate(db); err != nil {
		logger.Fatal("run migrations", "error", err)
	}

	ctx := context.Background()
	stats := &seedStats{}
	orgID, songs, people := seedSubjects(ctx, db, stats)

	store := scheduling.NewStore(db)
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

// seedSubjects resolves the signed-in PCO org (see MostRecentSession's doc
// comment for why it never refreshes that session's token), then either
// creates/reuses the canned songs and reads up to seedPersonCount real
// people from it (a usable session), or falls back to placeholder IDs for
// both (no usable session - printed here so it's the first thing dbseed
// says, not something to infer from "seed-song-" IDs further down). The
// returned organization id is that session's real PCO org id whenever a
// session was found, even in the placeholder-people case - a real org id
// still keeps every seeded row properly org-scoped.
func seedSubjects(ctx context.Context, db *sql.DB, stats *seedStats) (organizationID string, songs []seedSong, people []seedPerson) {
	reqCtx := ctx
	organizationID = placeholderOrgID
	sessionValid := false
	if session, err := auth.NewStore(db).MostRecentSession(ctx); err == nil {
		// A near-expired token is left alone rather than refreshed here -
		// see MostRecentSession - so a stale one just falls through to the
		// plain ctx below (and, from there, to an all-placeholder result).
		if time.Now().Add(5 * time.Minute).Before(session.TokenExpiresAt) {
			reqCtx = pco.WithAccessToken(ctx, session.AccessToken)
			organizationID = session.OrganizationID
			sessionValid = true
		}
	}

	if sessionValid {
		fmt.Println("Signed-in PCO session found - seeding against real Planning Center data.")
	} else {
		fmt.Println("No usable signed-in PCO session found (or its token has expired) - seeding with placeholder song/person IDs instead.")
		fmt.Println("Sign in once at http://localhost:8080/login, then re-run `go run ./cmd/dbseed` to seed against your real Planning Center songs and people instead.")
	}
	fmt.Println()

	if sessionValid {
		songs = ensureSeedSongs(reqCtx, stats)
		fmt.Println()
	}
	for i := len(songs); i < len(cannedSongs); i++ {
		songs = append(songs, placeholderSong(i))
	}

	if sessionValid {
		if resp, err := pco.GetPeople(reqCtx, &pco.PeopleParams{OrderBy: "last_name", PerPage: seedPersonCount}); err == nil {
			for _, p := range resp.Data {
				people = append(people, seedPerson{ID: p.ID, Name: p.Attributes.Name})
			}
		}
	}
	realPersonCount := len(people)
	for i := len(people); i < seedPersonCount; i++ {
		people = append(people, placeholderPerson(i))
	}
	if sessionValid && realPersonCount < seedPersonCount {
		fmt.Printf(
			"Note: your org only has %d real person/people to work with - padding the rest with placeholders (wanted %d for full variety).\n\n",
			realPersonCount, seedPersonCount,
		)
	}

	return organizationID, songs, people
}

func placeholderSong(i int) seedSong {
	id := fmt.Sprintf("seed-song-%d", i+1)
	return seedSong{ID: id, Title: id + " (placeholder)"}
}

func placeholderPerson(i int) seedPerson {
	id := fmt.Sprintf("seed-person-%d", i+1)
	return seedPerson{ID: id, Name: id + " (placeholder)"}
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
		logger.Error("seeding row failed", "action", action, "error", err)
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

// seedSongTags tags songs with example values for every category
// song_tag_categories actually has (see ListSongTagCategories) - a
// category added to that table later gets seeded here automatically, with
// one generic example (see the fallback below); the categories this seeder
// has real canned examples for get several each instead of just one, so
// the result reads like an actual tagged library rather than a bare
// minimum smoke test. Songs are picked by cycling through the available
// set, advancing across category boundaries so different songs pick up
// different tags rather than the same handful repeating.
func seedSongTags(ctx context.Context, store *scheduling.Store, stats *seedStats, organizationID string, songs []seedSong) {
	fmt.Println("Song tags:")
	categories, err := store.ListSongTagCategories(ctx)
	if err != nil {
		logger.Error("loading song tag categories failed", "error", err)
		return
	}

	// Several example values per category this seeder knows about today -
	// a category it doesn't recognize still gets one serviceable generic
	// example instead of being skipped.
	sampleValues := map[string][]string{
		scheduling.TagCategoryTheme:     {"Grace", "Redemption", "Hope", "Surrender"},
		scheduling.TagCategoryScripture: {"Romans 8:1", "Psalm 23", "John 3:16", "Isaiah 40:31"},
		scheduling.TagCategoryIdea:      {"Good for communion", "Good for baptisms", "High energy opener", "Reflective closer"},
	}

	songIndex := 0
	for _, cat := range categories {
		values := sampleValues[cat.Code]
		if len(values) == 0 {
			values = []string{cat.Label + " example"}
		}

		for _, value := range values {
			song := songs[songIndex%len(songs)]
			songIndex++

			_, err := store.CreateSongTag(ctx, organizationID, scheduling.CreateSongTagParams{SongID: song.ID, Category: cat.Code, Value: value})
			stats.report(fmt.Sprintf("%s: %q on %s", cat.Code, value, song.Title), err)
		}
	}
}

// seedRolesPerType is how many songs get each role - more than one so the
// set builder's role-filter buttons and "Find songs to add" list actually
// have more than a single candidate to choose from per role, the way a
// real library would.
const seedRolesPerType = 2

// seedSongRoles assigns every role song_role_types actually has (see
// ListSongRoleTypes - opener/response plus, as of migration 00020,
// communion/prayer/reflection/baptism) to seedRolesPerType songs each,
// cycling through the available songs the same way seedSongTags does - a
// song can and often will end up with more than one role, same as a real
// song might be both an opener and a communion song.
func seedSongRoles(ctx context.Context, store *scheduling.Store, stats *seedStats, organizationID string, songs []seedSong) {
	fmt.Println("Song roles:")
	types, err := store.ListSongRoleTypes(ctx)
	if err != nil {
		logger.Error("loading song role types failed", "error", err)
		return
	}

	songIndex := 0
	for _, t := range types {
		for range seedRolesPerType {
			song := songs[songIndex%len(songs)]
			songIndex++

			_, err := store.CreateSongRole(ctx, organizationID, scheduling.CreateSongRoleParams{SongID: song.ID, Role: t.Code})
			stats.report(fmt.Sprintf("%s: %s", t.Code, song.Title), err)
		}
	}
}

// seedSongRelationshipsPerType is how many relationship pairs get seeded
// per type - see seedSongRelationships for how these chain across the song
// list rather than just repeating one pair.
const seedSongRelationshipsPerType = 2

// seedSongRelationships relates songs for every relationship type
// song_relationship_types actually has (see ListSongRelationshipTypes),
// seedSongRelationshipsPerType pairs each. Pairs advance by one song per
// relationship rather than jumping by two, so consecutive relationships
// share a song and the whole thing reads as one connected web (song 1
// relates to song 2, song 2 to song 3, and so on) instead of a few
// disjoint, unrelated pairs - much closer to what an actual planned-out
// song library's relationships look like.
func seedSongRelationships(ctx context.Context, store *scheduling.Store, stats *seedStats, organizationID string, songs []seedSong) {
	fmt.Println("Song relationships:")
	if len(songs) < 2 {
		fmt.Println("  (skipped - need at least 2 songs)")
		return
	}

	types, err := store.ListSongRelationshipTypes(ctx)
	if err != nil {
		logger.Error("loading song relationship types failed", "error", err)
		return
	}

	pairIndex := 0
	for _, t := range types {
		for range seedSongRelationshipsPerType {
			a := songs[pairIndex%len(songs)]
			b := songs[(pairIndex+1)%len(songs)]
			pairIndex++

			_, err := store.CreateSongRelationship(ctx, organizationID, scheduling.CreateSongRelationshipParams{SongIDA: a.ID, SongIDB: b.ID, RelationshipType: t.Code})
			stats.report(fmt.Sprintf("%s: %s <-> %s", t.Code, a.Title, b.Title), err)
		}
	}
}

// seedPreferencesPerType is how many song/person pairings get seeded per
// preference type.
const seedPreferencesPerType = 3

// seedSongPreferences sets a song/person preference for every preference
// type song_preference_types actually has (see ListSongPreferenceTypes),
// seedPreferencesPerType pairings each, cycling through the available
// songs and people the same way the other seed functions do.
func seedSongPreferences(ctx context.Context, store *scheduling.Store, stats *seedStats, organizationID string, songs []seedSong, people []seedPerson) {
	fmt.Println("Song/person preferences:")
	types, err := store.ListSongPreferenceTypes(ctx)
	if err != nil {
		logger.Error("loading song preference types failed", "error", err)
		return
	}

	notes := map[string]string{
		scheduling.PreferencePreferred:   "Great vocal fit.",
		scheduling.PreferenceDiscouraged: "Key doesn't suit their range.",
	}

	i := 0
	for _, t := range types {
		note := notes[t.Code]
		if note == "" {
			note = t.Label + " example"
		}

		for range seedPreferencesPerType {
			song := songs[i%len(songs)]
			person := people[i%len(people)]
			i++

			_, err := store.SetSongPersonPreference(ctx, organizationID, scheduling.SetSongPersonPreferenceParams{
				SongID: song.ID, PersonID: person.ID, Preference: t.Code, Notes: note,
			})
			stats.report(fmt.Sprintf("%s: %s / %s", t.Code, song.Title, person.Name), err)
		}
	}
}

// seedPersonRelationshipsPerType is how many relationship pairs get seeded
// per type - see seedSongRelationships for the chaining pattern this
// mirrors.
const seedPersonRelationshipsPerType = 2

// seedPersonRelationships relates people for every relationship type
// person_relationship_types actually has (see ListPersonRelationshipTypes),
// seedPersonRelationshipsPerType pairs each, chained the same way
// seedSongRelationships is so the result reads as one connected web of
// relationships rather than a few disjoint pairs. A directional type's
// pair order (e.g. mentorship's "A mentors B") is preserved for whatever
// the chained pair happens to be.
func seedPersonRelationships(ctx context.Context, store *scheduling.Store, stats *seedStats, organizationID string, people []seedPerson) {
	fmt.Println("Person relationships:")
	if len(people) < 2 {
		fmt.Println("  (skipped - need at least 2 people)")
		return
	}

	types, err := store.ListPersonRelationshipTypes(ctx)
	if err != nil {
		logger.Error("loading person relationship types failed", "error", err)
		return
	}

	pairIndex := 0
	for _, t := range types {
		for range seedPersonRelationshipsPerType {
			a := people[pairIndex%len(people)]
			b := people[(pairIndex+1)%len(people)]
			pairIndex++

			_, err := store.CreatePersonRelationship(ctx, organizationID, scheduling.CreatePersonRelationshipParams{PersonIDA: a.ID, PersonIDB: b.ID, RelationshipType: t.Code})
			stats.report(fmt.Sprintf("%s: %s <-> %s", t.Code, a.Name, b.Name), err)
		}
	}
}
