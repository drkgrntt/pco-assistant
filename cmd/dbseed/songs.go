package main

import (
	"context"
	"fmt"
	"strings"

	pco "github.com/drkgrntt/go-pco"
)

// seedArrangementSpec is the key/tempo dbseed sets on a canned song's
// arrangement, mirroring what Planning Center's own "add song" flow leads
// you through by hand (pick a key, note a tempo) - see ensureSeedArrangement.
type seedArrangementSpec struct {
	Key   string
	BPM   float64
	Meter string
}

type seedSongSpec struct {
	Title, Author, Themes string
	Arrangement           seedArrangementSpec
}

// cannedSongs are the songs dbseed creates/reuses in Planning Center - a
// fixed, fictional set (not real copyrighted song titles) so a fresh
// pco-reset + dbseed cycle always produces the same recognizable library,
// every entry with a real key/tempo set so the "arrangement" side of a real
// song actually looks lived-in rather than PCO's bare "Default Arrangement"
// default.
var cannedSongs = []seedSongSpec{
	{Title: "Great and Faithful", Themes: "Faithfulness, Worship", Arrangement: seedArrangementSpec{Key: "G", BPM: 72, Meter: "4/4"}},
	{Title: "Song of Ascent", Themes: "Praise, Celebration", Arrangement: seedArrangementSpec{Key: "D", BPM: 140, Meter: "4/4"}},
	{Title: "Unshaken Hope", Themes: "Hope, Trust", Arrangement: seedArrangementSpec{Key: "A", BPM: 76, Meter: "3/4"}},
	{Title: "Rescued and Redeemed", Themes: "Redemption, Grace", Arrangement: seedArrangementSpec{Key: "E", BPM: 128, Meter: "4/4"}},
	{Title: "Still My Soul", Themes: "Peace, Surrender", Arrangement: seedArrangementSpec{Key: "C", BPM: 68, Meter: "4/4"}},
	{Title: "Overflow of Praise", Themes: "Praise, Joy", Arrangement: seedArrangementSpec{Key: "B", BPM: 120, Meter: "4/4"}},
	{Title: "Anchor and Anthem", Themes: "Hope, Declaration", Arrangement: seedArrangementSpec{Key: "F", BPM: 84, Meter: "6/8"}},
	{Title: "Home at Last", Themes: "Heaven, Hope", Arrangement: seedArrangementSpec{Key: "Bb", BPM: 96, Meter: "4/4"}},
}

// ensureSeedSongs creates each of cannedSongs in Planning Center (skipping
// any that already exist by title, so a repeat run doesn't duplicate the
// library) and makes sure each one's auto-created default arrangement (see
// ensureSeedArrangement) carries the canned key/tempo. Returns one seedSong
// per cannedSongs entry it managed to create or find - a song whose
// creation genuinely failed (a real PCO error, not "already exists") is
// simply left out rather than padded with a placeholder, since every other
// entry already succeeded against the real org.
func ensureSeedSongs(ctx context.Context, stats *seedStats) []seedSong {
	fmt.Println("Songs (Planning Center):")

	existingByTitle := map[string]string{}
	if existing, err := pco.GetSongs(ctx, &pco.SongsParams{PerPage: 100}); err == nil {
		for _, s := range existing.Data {
			existingByTitle[strings.ToLower(s.Attributes.Title)] = s.ID
		}
	}

	var songs []seedSong
	for _, spec := range cannedSongs {
		songID, ok := existingByTitle[strings.ToLower(spec.Title)]
		if ok {
			stats.report("song: "+spec.Title, fmt.Errorf("already exists"))
		} else {
			created, err := pco.CreateSong(ctx, &pco.CreateSongParams{Title: spec.Title, Themes: spec.Themes})
			stats.report("song: "+spec.Title, err)
			if err != nil {
				continue
			}
			songID = created.Data.ID
		}

		ensureSeedArrangement(ctx, stats, songID, spec)
		songs = append(songs, seedSong{ID: songID, Title: spec.Title})
	}

	return songs
}

// ensureSeedArrangement sets the canned key/bpm/meter on songID's
// auto-created default arrangement (see UpdateArrangement's doc comment in
// go-pco - Planning Center creates one "Default Arrangement" with every
// blank field the moment the song itself is created, so this updates that
// one rather than creating a second). Idempotent by inspection rather than
// a stored flag: if the arrangement's chord_chart_key already matches, this
// run's update/key-creation is skipped and reported as already-seeded, the
// same convention as the relationship tables in main.go.
func ensureSeedArrangement(ctx context.Context, stats *seedStats, songID string, spec seedSongSpec) {
	arrangements, err := pco.GetArrangements(ctx, songID, nil)
	if err != nil || len(arrangements.Data) == 0 {
		stats.report("arrangement: "+spec.Title, fmt.Errorf("no arrangement found to update (%v)", err))
		return
	}
	arrangement := arrangements.Data[0]

	label := fmt.Sprintf("arrangement: %s (key %s, %.0f bpm)", spec.Title, spec.Arrangement.Key, spec.Arrangement.BPM)
	if arrangement.Attributes.ChordChartKey == spec.Arrangement.Key {
		stats.report(label, fmt.Errorf("already exists"))
	} else {
		_, err := pco.UpdateArrangement(ctx, songID, arrangement.ID, &pco.UpdateArrangementParams{
			ChordChartKey: spec.Arrangement.Key,
			BPM:           spec.Arrangement.BPM,
			Meter:         spec.Arrangement.Meter,
		})
		stats.report(label, err)
	}

	keyLabel := fmt.Sprintf("key: %s (%s)", spec.Title, spec.Arrangement.Key)
	if keys, err := pco.GetKeys(ctx, songID, arrangement.ID, nil); err != nil {
		stats.report(keyLabel, err)
	} else if len(keys.Data) > 0 {
		stats.report(keyLabel, fmt.Errorf("already exists"))
	} else {
		_, err := pco.CreateKey(ctx, songID, arrangement.ID, &pco.CreateKeyParams{StartingKey: spec.Arrangement.Key})
		stats.report(keyLabel, err)
	}
}
