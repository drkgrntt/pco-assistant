// Command pcoreset deletes every Song and Plan in the signed-in Planning
// Center organization - the repeatable counterpart to dbreset/dbseed for
// the *live PCO account* side of a reseed cycle, since those two only ever
// touch this app's own local database (songs/people/plans themselves live
// in Planning Center, not here). Meant for a disposable dev/test PCO
// account you're happy to empty out and re-seed, over and over, while
// building - never point this at a real church's account.
//
// This only clears Planning Center itself. Local rows that reference the
// songs/plans it deletes (song tags/roles/relationships, sets, rosters,
// tracked_new_songs, first_song_anchors, ...) are left behind, now
// pointing at IDs that no longer exist - run `go run ./cmd/dbreset`
// afterward (or before re-seeding) to clear those out too.
//
// Run it with:
//
//	go run ./cmd/pcoreset
//
// or, to skip the confirmation prompt (e.g. from another script):
//
//	go run ./cmd/pcoreset -yes
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"pco-assistant/internal/auth"
	"pco-assistant/internal/database"

	pco "github.com/drkgrntt/go-pco"
	_ "github.com/joho/godotenv/autoload"
)

// pcoresetPageSize/pcoresetMaxPages mirror fetchAllSongs' pagination
// pattern (internal/server/controllers/scheduling.go) - a page size PCO
// comfortably handles per request, capped at enough pages to cover even a
// large dev org without looping forever if Meta.TotalCount is ever wrong.
const (
	pcoresetPageSize = 100
	pcoresetMaxPages = 50
)

func main() {
	skipConfirm := flag.Bool("yes", false, "skip the confirmation prompt")
	flag.Parse()

	// Same guardrail as dbreset/dbseed - this is a destructive, effectively
	// irreversible action against a real Planning Center account, so it
	// refuses to run unless APP_ENV explicitly says this is a disposable
	// setup. Unlike dbreset, "irreversible" here isn't a figure of speech:
	// there's no local database to just re-migrate from - once a Song or
	// Plan is deleted from Planning Center itself, it's gone.
	if os.Getenv("APP_ENV") != "local" {
		log.Fatal("pcoreset refuses to run unless APP_ENV=local - it permanently deletes real Planning Center Songs and Plans, and should only ever be pointed at a disposable dev/test PCO account")
	}

	db, err := database.Open()
	if err != nil {
		log.Fatalf("connect to database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	session, err := auth.NewStore(db).MostRecentSession(ctx)
	if err != nil {
		log.Fatalf("no signed-in session found (%v) - sign in at http://localhost:8080/login first, pcoreset acts as whichever PCO user last signed in", err)
	}
	if !time.Now().Add(5 * time.Minute).Before(session.TokenExpiresAt) {
		log.Fatal("the most recent signed-in session's token is expired or about to expire - sign in again at http://localhost:8080/login, then re-run pcoreset")
	}
	reqCtx := pco.WithAccessToken(ctx, session.AccessToken)

	fmt.Printf("Signed in as %s, organization %q. Looking up what's there to delete...\n", session.PersonName, session.OrganizationName)

	plans, err := allPlans(reqCtx)
	if err != nil {
		log.Fatalf("listing plans: %v", err)
	}
	songs, err := allSongs(reqCtx)
	if err != nil {
		log.Fatalf("listing songs: %v", err)
	}

	if len(plans) == 0 && len(songs) == 0 {
		fmt.Println("Nothing to delete - no Plans or Songs found in this organization.")
		return
	}

	fmt.Printf("Found %d plan(s) and %d song(s) to permanently delete:\n", len(plans), len(songs))
	for _, p := range plans {
		fmt.Printf("  - [plan] %s (%s)\n", p.title, p.dates)
	}
	for _, s := range songs {
		fmt.Printf("  - [song] %s\n", s.title)
	}

	if !*skipConfirm && !confirm(session.OrganizationName) {
		fmt.Println("Aborted - nothing was changed.")
		os.Exit(1)
	}

	deleted, failed := 0, 0
	for _, p := range plans {
		if err := pco.DeletePlan(reqCtx, p.serviceTypeID, p.id); err != nil {
			fmt.Printf("FAILED to delete plan %q: %v\n", p.title, err)
			failed++
			continue
		}
		deleted++
	}
	for _, s := range songs {
		if err := pco.DeleteSong(reqCtx, s.id); err != nil {
			fmt.Printf("FAILED to delete song %q: %v\n", s.title, err)
			failed++
			continue
		}
		deleted++
	}

	fmt.Printf("\nDone: %d deleted, %d failed.\n", deleted, failed)
	fmt.Println("Local rows referencing these (song tags/roles/relationships, sets, rosters, ...) are now stale - run `go run ./cmd/dbreset` before re-seeding.")
}

func confirm(orgName string) bool {
	fmt.Printf("This will PERMANENTLY delete all of the above from the live Planning Center organization %q. Type \"reset\" to continue: ", orgName)

	answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimSpace(answer) == "reset"
}

type planToDelete struct {
	id            string
	serviceTypeID string
	title         string
	dates         string
}

// allPlans walks every Service Type and, within each, every Plan - both
// past and future, since PlansParams.Filter only ever narrows to one or the
// other and there's no single call that returns everything at once (see
// recentlyRepeatedSongs' doc comment on the same limitation).
func allPlans(ctx context.Context) ([]planToDelete, error) {
	serviceTypes, err := allServiceTypes(ctx)
	if err != nil {
		return nil, err
	}

	var plans []planToDelete
	for _, st := range serviceTypes {
		for _, filter := range []string{"past", "future"} {
			for page := 0; page < pcoresetMaxPages; page++ {
				resp, err := pco.GetPlans(ctx, st.ID, &pco.PlansParams{
					Filter:  filter,
					PerPage: pcoresetPageSize,
					Offset:  page * pcoresetPageSize,
				})
				if err != nil {
					return nil, fmt.Errorf("listing %s plans for service type %q: %w", filter, st.Attributes.Name, err)
				}

				for _, p := range resp.Data {
					plans = append(plans, planToDelete{
						id:            p.ID,
						serviceTypeID: st.ID,
						title:         fmt.Sprintf("%s / %s", st.Attributes.Name, p.Attributes.Title),
						dates:         p.Attributes.Dates,
					})
				}

				if len(resp.Data) < pcoresetPageSize {
					break
				}
			}
		}
	}

	return plans, nil
}

func allServiceTypes(ctx context.Context) ([]pco.ServiceTypeData, error) {
	var serviceTypes []pco.ServiceTypeData

	for page := 0; page < pcoresetMaxPages; page++ {
		resp, err := pco.GetServiceTypes(ctx, &pco.ServiceTypesParams{
			PerPage: pcoresetPageSize,
			Offset:  page * pcoresetPageSize,
		})
		if err != nil {
			return nil, err
		}

		serviceTypes = append(serviceTypes, resp.Data...)

		if len(resp.Data) < pcoresetPageSize {
			break
		}
	}

	return serviceTypes, nil
}

type songToDelete struct {
	id    string
	title string
}

func allSongs(ctx context.Context) ([]songToDelete, error) {
	var songs []songToDelete

	for page := 0; page < pcoresetMaxPages; page++ {
		resp, err := pco.GetSongs(ctx, &pco.SongsParams{
			OrderBy: "title",
			PerPage: pcoresetPageSize,
			Offset:  page * pcoresetPageSize,
		})
		if err != nil {
			return nil, err
		}

		for _, s := range resp.Data {
			songs = append(songs, songToDelete{id: s.ID, title: s.Attributes.Title})
		}

		if len(resp.Data) < pcoresetPageSize {
			break
		}
	}

	return songs, nil
}
