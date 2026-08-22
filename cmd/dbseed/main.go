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
// PCO access token and seeds against a handful of your org's real songs and
// people, so titles/names show up properly in the UI. Otherwise it falls
// back to obviously-fake placeholder IDs ("seed-song-1" etc.), which still
// exercise every page, just without a resolvable title/name.
//
// Safe to re-run: every table it seeds either upserts (song roles/tags,
// song/person preferences) or quietly skips a row that's already there
// (song/person relationships), so running it again doesn't duplicate data.
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

// seedSongCount/seedPersonCount are how many real songs/people dbseed tries
// to pull from PCO before deciding it has enough to work with - matched to
// the highest index the seed*/functions below actually reference.
const (
	seedSongCount   = 4
	seedPersonCount = 3
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
	orgID, songs, people, live := seedSubjects(ctx, db)
	if live {
		fmt.Printf("Signed-in PCO session found - seeding against %d real song(s) and %d real person/people.\n", len(songs), len(people))
	} else {
		fmt.Println("No usable signed-in PCO session found - seeding with placeholder song/person IDs instead.")
		fmt.Println("Sign in once at http://localhost:8080/login, then re-run `go run ./cmd/dbseed` to seed against your real Planning Center songs and people instead.")
	}
	fmt.Println()

	store := scheduling.NewStore(db)
	seedSongTags(ctx, store, orgID, songs)
	seedSongRoles(ctx, store, orgID, songs)
	seedSongRelationships(ctx, store, orgID, songs)
	seedSongPreferences(ctx, store, orgID, songs, people)
	seedPersonRelationships(ctx, store, orgID, people)

	fmt.Println("\nSeeding complete.")
}

// placeholderOrgID stands in for a real PCO organization id when there's no
// usable signed-in session to borrow one from (see seedSubjects) - every
// scheduling.Store method now requires an organization id (see migration
// 00019), so seeded rows need *some* value even in the placeholder case.
const placeholderOrgID = "seed-org"

// seedSubjects tries to gather seedSongCount songs and seedPersonCount
// people from the signed-in PCO org (see MostRecentSession's doc comment
// for why it never refreshes that session's token), falling back to
// synthetic placeholder IDs if there's no usable session, the API calls
// fail, or the org just doesn't have enough songs/people yet. The returned
// organization id is that session's real PCO org id in the live case, or
// placeholderOrgID otherwise.
func seedSubjects(ctx context.Context, db *sql.DB) (organizationID string, songs []seedSong, people []seedPerson, live bool) {
	reqCtx := ctx
	organizationID = placeholderOrgID
	if session, err := auth.NewStore(db).MostRecentSession(ctx); err == nil {
		// A near-expired token is left alone rather than refreshed here -
		// see MostRecentSession - so a stale one just falls through to the
		// plain ctx below (and, from there, to the placeholder fallback).
		if time.Now().Add(5 * time.Minute).Before(session.TokenExpiresAt) {
			reqCtx = pco.WithAccessToken(ctx, session.AccessToken)
			organizationID = session.OrganizationID
		}
	}

	songResp, songErr := pco.GetSongs(reqCtx, &pco.SongsParams{OrderBy: "title", PerPage: seedSongCount})
	peopleResp, peopleErr := pco.GetPeople(reqCtx, &pco.PeopleParams{OrderBy: "last_name", PerPage: seedPersonCount})

	if songErr == nil && peopleErr == nil && len(songResp.Data) >= seedSongCount && len(peopleResp.Data) >= seedPersonCount {
		for _, s := range songResp.Data[:seedSongCount] {
			songs = append(songs, seedSong{ID: s.ID, Title: s.Attributes.Title})
		}
		for _, p := range peopleResp.Data[:seedPersonCount] {
			people = append(people, seedPerson{ID: p.ID, Name: p.Attributes.Name})
		}
		return organizationID, songs, people, true
	}

	return placeholderOrgID, placeholderSongs(), placeholderPeople(), false
}

func placeholderSongs() []seedSong {
	songs := make([]seedSong, seedSongCount)
	for i := range songs {
		id := fmt.Sprintf("seed-song-%d", i+1)
		songs[i] = seedSong{ID: id, Title: id + " (placeholder)"}
	}
	return songs
}

func placeholderPeople() []seedPerson {
	people := make([]seedPerson, seedPersonCount)
	for i := range people {
		id := fmt.Sprintf("seed-person-%d", i+1)
		people[i] = seedPerson{ID: id, Name: id + " (placeholder)"}
	}
	return people
}

// seedSongTags adds a couple of theme/scripture/idea tags (see
// internal/scheduling/song_tags.go) across the first three seed songs.
func seedSongTags(ctx context.Context, store *scheduling.Store, organizationID string, songs []seedSong) {
	fmt.Println("Song tags:")
	for _, t := range []scheduling.CreateSongTagParams{
		{SongID: songs[0].ID, Category: scheduling.TagCategoryTheme, Value: "Grace"},
		{SongID: songs[0].ID, Category: scheduling.TagCategoryScripture, Value: "Romans 8:1"},
		{SongID: songs[1].ID, Category: scheduling.TagCategoryTheme, Value: "New beginnings"},
		{SongID: songs[2].ID, Category: scheduling.TagCategoryIdea, Value: "Good for baptisms"},
	} {
		_, err := store.CreateSongTag(ctx, organizationID, t)
		report(fmt.Sprintf("%s: %q on %s", t.Category, t.Value, songs[songIndex(songs, t.SongID)].Title), err)
	}
}

// seedSongRoles tags the first song as an opener and the second as a
// response song (see internal/scheduling/song_roles.go) - the two roles the
// set builder's candidate filtering actually looks for.
func seedSongRoles(ctx context.Context, store *scheduling.Store, organizationID string, songs []seedSong) {
	fmt.Println("Song roles:")
	for _, r := range []scheduling.CreateSongRoleParams{
		{SongID: songs[0].ID, Role: scheduling.RoleOpener},
		{SongID: songs[1].ID, Role: scheduling.RoleResponse},
	} {
		_, err := store.CreateSongRole(ctx, organizationID, r)
		report(fmt.Sprintf("%s: %s", r.Role, songs[songIndex(songs, r.SongID)].Title), err)
	}
}

// seedSongRelationships adds one relationship of each type the set
// builder's suggestion/conflict logic recognizes (see
// internal/scheduling/song_relationships.go).
func seedSongRelationships(ctx context.Context, store *scheduling.Store, organizationID string, songs []seedSong) {
	fmt.Println("Song relationships:")
	for _, r := range []scheduling.CreateSongRelationshipParams{
		{SongIDA: songs[0].ID, SongIDB: songs[1].ID, RelationshipType: scheduling.RelationshipFlowsWell},
		{SongIDA: songs[2].ID, SongIDB: songs[3].ID, RelationshipType: scheduling.RelationshipSameTheme},
		{SongIDA: songs[0].ID, SongIDB: songs[2].ID, RelationshipType: scheduling.RelationshipAvoidPairing},
	} {
		_, err := store.CreateSongRelationship(ctx, organizationID, r)
		report(fmt.Sprintf("%s: %s <-> %s", r.RelationshipType, songs[songIndex(songs, r.SongIDA)].Title, songs[songIndex(songs, r.SongIDB)].Title), err)
	}
}

// seedSongPreferences adds one preferred and one discouraged song/person
// pairing (see internal/scheduling/song_preferences.go).
func seedSongPreferences(ctx context.Context, store *scheduling.Store, organizationID string, songs []seedSong, people []seedPerson) {
	fmt.Println("Song/person preferences:")
	for _, p := range []scheduling.SetSongPersonPreferenceParams{
		{SongID: songs[0].ID, PersonID: people[0].ID, Preference: scheduling.PreferencePreferred, Notes: "Great vocal fit."},
		{SongID: songs[3].ID, PersonID: people[1].ID, Preference: scheduling.PreferenceDiscouraged, Notes: "Key doesn't suit their range."},
	} {
		_, err := store.SetSongPersonPreference(ctx, organizationID, p)
		report(fmt.Sprintf("%s: %s / %s", p.Preference, songs[songIndex(songs, p.SongID)].Title, people[personIndex(people, p.PersonID)].Name), err)
	}
}

// seedPersonRelationships adds one relationship of each type the roster
// builder's suggestion/conflict logic recognizes (see
// internal/scheduling/person_relationships.go).
func seedPersonRelationships(ctx context.Context, store *scheduling.Store, organizationID string, people []seedPerson) {
	fmt.Println("Person relationships:")
	for _, r := range []scheduling.CreatePersonRelationshipParams{
		{PersonIDA: people[0].ID, PersonIDB: people[1].ID, RelationshipType: scheduling.PersonRelationshipWorksWell},
		{PersonIDA: people[0].ID, PersonIDB: people[2].ID, RelationshipType: scheduling.PersonRelationshipMentorship},
		{PersonIDA: people[1].ID, PersonIDB: people[2].ID, RelationshipType: scheduling.PersonRelationshipAvoidPairing},
	} {
		_, err := store.CreatePersonRelationship(ctx, organizationID, r)
		report(fmt.Sprintf("%s: %s <-> %s", r.RelationshipType, people[personIndex(people, r.PersonIDA)].Name, people[personIndex(people, r.PersonIDB)].Name), err)
	}
}

// report prints one seeded row's outcome. CreateSongRelationship/
// CreatePersonRelationship's ON CONFLICT DO NOTHING surfaces a re-run's
// already-seeded rows as an "already exists" error rather than silently
// succeeding (see their doc comments) - that's the one error this treats as
// an expected no-op instead of a failure, so dbseed stays safe to re-run.
func report(action string, err error) {
	switch {
	case err == nil:
		fmt.Println("  +", action)
	case strings.Contains(err.Error(), "already exists"):
		fmt.Println("  =", action, "(already seeded)")
	default:
		log.Printf("  ! %s: %v", action, err)
	}
}

func songIndex(songs []seedSong, id string) int {
	for i, s := range songs {
		if s.ID == id {
			return i
		}
	}
	return -1
}

func personIndex(people []seedPerson, id string) int {
	for i, p := range people {
		if p.ID == id {
			return i
		}
	}
	return -1
}
