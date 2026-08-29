package scheduling

import (
	"context"

	pco "github.com/drkgrntt/go-pco"
)

// pcoSongPageSize is PCO's max per_page, and pcoSongMaxPages caps how many
// pages FetchAllSongs will walk - a safety valve so an unexpectedly huge
// library can't turn one call into an unbounded number of PCO requests.
const (
	pcoSongPageSize = 100
	pcoSongMaxPages = 20
)

// FetchAllSongs pages through the PCO song library, ordered by title.
// Moved here (from the controllers package, which still has its own
// fetchAllSongs delegating to this) so middleware.PopulateNavState can
// fetch the same library without controllers importing middleware or vice
// versa. Returns whatever it managed to fetch even on a mid-pagination
// error, since a stale-but-nonempty result beats an empty one.
func FetchAllSongs(ctx context.Context) ([]pco.SongData, error) {
	var songs []pco.SongData

	for page := 0; page < pcoSongMaxPages; page++ {
		resp, err := pco.GetSongs(ctx, &pco.SongsParams{
			OrderBy: "title",
			PerPage: pcoSongPageSize,
			Offset:  page * pcoSongPageSize,
		})
		if err != nil {
			return songs, err
		}

		songs = append(songs, resp.Data...)

		if len(resp.Data) < pcoSongPageSize || len(songs) >= resp.Meta.TotalCount {
			break
		}
	}

	return songs, nil
}
