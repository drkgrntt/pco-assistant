package scheduling

// "PCO Tags" - the additive, read+assign lens onto real PCO song-scoped
// Tags/TagGroups (see docs/plans/in-progress/pco-native-tags-plan.md).
// Deliberately separate from song_tags.go/song_contexts.go: those stay
// exactly as they are (local tables, renamed to "Song Labels" in the UI -
// see that plan's final decision), this file is a thin, no-local-table
// passthrough to live PCO data, mirroring pco_songs.go's own shape.

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	pco "github.com/drkgrntt/go-pco"
)

// PCOTagGroup is one real PCO TagGroup with TagsFor == "song" - PCO also
// has person/media/arrangement-scoped groups, filtered out before this
// type is ever constructed (see pcoTagVocab below). Deliberately not named
// SongTagGroup - see the plan doc's Handoff notes for why the PCO prefix
// disambiguates from this package's existing, unrelated SongTag/
// SongTagCategory local types.
type PCOTagGroup struct {
	ID                      string
	Name                    string
	AllowMultipleSelections bool
	Required                bool
}

// PCOTag is one real PCO Tag - either one of a group's own available
// options (ListPCOTagGroupTags) or one currently assigned to a song
// (ListPCOSongTags/ListPCOSongTagsForOrg). PCO's own Tag resource doesn't
// distinguish "available" from "assigned", so both cases share this one
// shape - GroupID is always populated (from the Tag's own TagGroup
// relationship), not just when it's convenient for the caller. Also reused
// as-is by pco_person_tags.go's ListPCOPersonTags - same PCO resource
// shape regardless of whether the tag's own group is song- or
// person-scoped, so there's no separate PersonTag type.
type PCOTag struct {
	ID      string
	GroupID string
	Name    string
}

// pcoTagVocabEntry is one organization's cached song-scoped tag-group
// vocabulary: group metadata plus every group's own available tags,
// fetched together in a single GetTagGroups(Include: []string{"tags"})
// call - see pcoTagVocab. groupTags lets ListPCOTagGroupTags answer from
// this same cache entry instead of a second call per group.
type pcoTagVocabEntry struct {
	groups    []PCOTagGroup
	groupTags map[string][]PCOTag
	cachedAt  time.Time
}

var (
	pcoTagVocabMu    sync.Mutex
	pcoTagVocabCache = map[string]pcoTagVocabEntry{}
)

// pcoTagVocab is ListPCOTagGroups/ListPCOTagGroupTags' shared cache-aside
// fetch - same shape and TTL as FetchActiveSongs (pco_songs.go, reusing
// its songCacheTTL/songCacheEntryFresh rather than a second constant/copy
// of the same 3-line freshness check): an org's tag vocabulary is exactly
// as session-stable as its song library, so the same staleness tolerance
// applies. Only a successful fetch is cached, same fail-safe convention as
// every other cache in this app.
func (s *Store) pcoTagVocab(ctx context.Context, organizationID string) (pcoTagVocabEntry, error) {
	now := time.Now()

	pcoTagVocabMu.Lock()
	entry, ok := pcoTagVocabCache[organizationID]
	pcoTagVocabMu.Unlock()
	if ok && songCacheEntryFresh(entry.cachedAt, now, songCacheTTL) {
		return entry, nil
	}

	resp, err := pco.GetTagGroups(ctx, &pco.TagGroupsParams{Include: []string{"tags"}})
	if err != nil {
		return pcoTagVocabEntry{}, err
	}

	tagsByGroup := tagsFromIncluded(resp.Included)

	groups := make([]PCOTagGroup, 0, len(resp.Data))
	groupTags := make(map[string][]PCOTag, len(resp.Data))
	for _, g := range resp.Data {
		if g.Attributes.TagsFor != "song" {
			continue
		}
		groups = append(groups, PCOTagGroup{
			ID:                      g.ID,
			Name:                    g.Attributes.Name,
			AllowMultipleSelections: g.Attributes.AllowMultipleSelections,
			Required:                g.Attributes.Required,
		})
		groupTags[g.ID] = tagsByGroup[g.ID]
	}

	fresh := pcoTagVocabEntry{groups: groups, groupTags: groupTags, cachedAt: now}
	pcoTagVocabMu.Lock()
	pcoTagVocabCache[organizationID] = fresh
	pcoTagVocabMu.Unlock()

	return fresh, nil
}

// tagsFromIncluded extracts every sideloaded Tag from a
// GetTagGroups(Include: []string{"tags"}) response's Included array,
// bucketed by owning tag-group id (TagData.Relationships.TagGroup) - the
// same json.Marshal-then-Unmarshal-by-type idiom
// itemNotesFromIncluded/itemAssignmentsFromIncluded
// (internal/server/controllers/sets_builder.go) already use for a go-pco
// Included sideload, matched here rather than invented fresh. Best-effort:
// a malformed entry, or one with no TagGroup relationship to bucket by, is
// silently skipped.
func tagsFromIncluded(included []any) map[string][]PCOTag {
	byGroup := map[string][]PCOTag{}
	for _, raw := range included {
		b, err := json.Marshal(raw)
		if err != nil {
			continue
		}
		var tag pco.TagData
		if err := json.Unmarshal(b, &tag); err != nil {
			continue
		}
		if tag.Type != "Tag" || tag.ID == "" {
			continue
		}
		groupRef := tag.Relationships.TagGroup.Data
		if groupRef == nil || groupRef.ID == "" {
			continue
		}
		byGroup[groupRef.ID] = append(byGroup[groupRef.ID], PCOTag{ID: tag.ID, GroupID: groupRef.ID, Name: tag.Attributes.Name})
	}
	return byGroup
}

// ListPCOTagGroups lists organizationID's song-scoped PCO tag groups
// (TagGroupAttributes.TagsFor == "song") - cached ~60s per org, see
// pcoTagVocab.
func (s *Store) ListPCOTagGroups(ctx context.Context, organizationID string) ([]PCOTagGroup, error) {
	entry, err := s.pcoTagVocab(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	return entry.groups, nil
}

// ListPCOTagGroupTags lists groupID's own available tags - reuses whatever
// ListPCOTagGroups already sideloaded for organizationID this TTL window
// (see pcoTagVocab) rather than a second GetTags call.
func (s *Store) ListPCOTagGroupTags(ctx context.Context, organizationID, groupID string) ([]PCOTag, error) {
	entry, err := s.pcoTagVocab(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	return entry.groupTags[groupID], nil
}

// ListPCOSongTags lists songID's currently-assigned PCO tags across every
// tag group. Deliberately NOT cached, unlike ListPCOTagGroups/
// ListPCOSongTagsForOrg above/below - this must stay live for correctness,
// the same reasoning CLAUDE.md's PCO API calls section gives for the
// roster's own short-TTL team-membership cache: assigning a tag
// (AssignPCOTagGroup) is exactly the action this needs to reflect back
// immediately, so its only real caller is right after a write, never a
// candidate-list render (see ListPCOSongTagsForOrg for that case).
func (s *Store) ListPCOSongTags(ctx context.Context, songID string) ([]PCOTag, error) {
	resp, err := pco.GetSongTags(ctx, songID)
	if err != nil {
		return nil, err
	}

	tags := make([]PCOTag, 0, len(resp.Data))
	for _, t := range resp.Data {
		groupID := ""
		if t.Relationships.TagGroup.Data != nil {
			groupID = t.Relationships.TagGroup.Data.ID
		}
		tags = append(tags, PCOTag{ID: t.ID, GroupID: groupID, Name: t.Attributes.Name})
	}
	return tags, nil
}

// AssignPCOTagGroup sets songID's tags for groupID to exactly tagIDs - a
// thin wrapper over go-pco's AssignSongTags (groupID itself is only for
// this call's own bookkeeping/doc clarity; AssignSongTags infers the group
// from the tag ids themselves, per its own doc comment).
//
// FULL REPLACE, not additive - repeating AssignSongTags' own warning at
// this layer too, since it's easy to miss one layer down: the caller
// (AssignPCOSongTag, internal/server/controllers/sets_detail.go) must
// compute the *complete* desired tag-id set for this one group before
// calling this, never just the single tag that changed. An empty tagIDs
// clears the group's assignment on the song entirely.
func (s *Store) AssignPCOTagGroup(ctx context.Context, songID, groupID string, tagIDs []string) error {
	return pco.AssignSongTags(ctx, songID, tagIDs)
}

// pcoSongTagsEntry/pcoSongTagsCache mirror pcoTagVocabEntry/
// pcoTagVocabCache's cache-aside shape for ListPCOSongTagsForOrg below -
// one organization's whole song-id -> assigned-PCO-tags map, same ~60s TTL.
type pcoSongTagsEntry struct {
	bySong   map[string][]PCOTag
	cachedAt time.Time
}

var (
	pcoSongTagsMu    sync.Mutex
	pcoSongTagsCache = map[string]pcoSongTagsEntry{}
)

// ListPCOSongTagsForOrg builds organizationID's whole song-id -> assigned
// PCO-tags map in one pass - the reference lookup for annotating a
// candidate-list render with each song's PCO tags
// (buildCandidateLookups, internal/server/controllers/sets_builder.go)
// without an N-calls-per-song loop.
//
// Shape: one Songs#index call per tag in tagGroups (where[song_tag_ids]=
// <tagID>, via pco.SongsParams.SongTagIDs, paged by Meta.TotalCount, not a
// silent page cap), fanned out concurrently via RunConcurrently/
// PCOFanOutLimit - NOT one call per song. An org typically has a handful
// of tags (~15-20, confirmed against a real dev org - see
// docs/plans/in-progress/pco-native-tags-plan.md's open question 1)
// relative to its song count (hundreds+), so this is bounded and cheap
// regardless of library size - the same "batch by filter value, not by
// row" shape FetchActiveSongs' own Hidden filter and personNameMapForIDs'
// where[id]=... batching already use elsewhere in this app.
//
// Cached per org, same TTL/cache-aside shape as ListPCOTagGroups
// (pcoTagVocab) - a view of who's tagged what that's stale by up to 60s is
// an acceptable tradeoff for a candidate-list annotation, the same
// tradeoff FetchActiveSongs itself already makes for the song list this is
// annotating. Never used for "does this one song have this one tag right
// after I just changed it" - that's ListPCOSongTags' job, deliberately
// uncached.
func (s *Store) ListPCOSongTagsForOrg(ctx context.Context, organizationID string, tagGroups []PCOTagGroup) (map[string][]PCOTag, error) {
	now := time.Now()

	pcoSongTagsMu.Lock()
	entry, ok := pcoSongTagsCache[organizationID]
	pcoSongTagsMu.Unlock()
	if ok && songCacheEntryFresh(entry.cachedAt, now, songCacheTTL) {
		return entry.bySong, nil
	}

	var allTags []PCOTag
	for _, g := range tagGroups {
		groupTags, err := s.ListPCOTagGroupTags(ctx, organizationID, g.ID)
		if err != nil {
			return nil, err
		}
		allTags = append(allTags, groupTags...)
	}

	var (
		mu     sync.Mutex
		fanErr error
	)
	bySong := map[string][]PCOTag{}
	RunConcurrently(allTags, func(tag PCOTag) {
		songIDs, err := fetchSongIDsTaggedWith(ctx, tag.ID)
		if err != nil {
			mu.Lock()
			if fanErr == nil {
				fanErr = err
			}
			mu.Unlock()
			return
		}
		mu.Lock()
		for _, id := range songIDs {
			bySong[id] = append(bySong[id], tag)
		}
		mu.Unlock()
	})
	if fanErr != nil {
		return nil, fanErr
	}

	pcoSongTagsMu.Lock()
	pcoSongTagsCache[organizationID] = pcoSongTagsEntry{bySong: bySong, cachedAt: now}
	pcoSongTagsMu.Unlock()

	return bySong, nil
}

// fetchSongIDsTaggedWith pages through every song carrying tagID
// (where[song_tag_ids]=tagID), driven by Meta.TotalCount like
// fetchSongsFromPCO's own paging - bounded by pcoSongSanityMaxPages, the
// same runaway/bug guard fetchSongsFromPCO uses, since one tag's own
// result set is expected to be a small fraction of the library, not close
// to it.
func fetchSongIDsTaggedWith(ctx context.Context, tagID string) ([]string, error) {
	var ids []string
	offset := 0
	for page := 0; page < pcoSongSanityMaxPages; page++ {
		resp, err := pco.GetSongs(ctx, &pco.SongsParams{
			SongTagIDs: []string{tagID},
			PerPage:    pcoSongPageSize,
			Offset:     offset,
		})
		if err != nil {
			return nil, err
		}
		for _, song := range resp.Data {
			ids = append(ids, song.ID)
		}
		offset += pcoSongPageSize
		if offset >= resp.Meta.TotalCount || len(resp.Data) == 0 {
			break
		}
	}
	return ids, nil
}
