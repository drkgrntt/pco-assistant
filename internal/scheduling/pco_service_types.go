package scheduling

import (
	"context"
	"sync"
	"time"

	pco "github.com/drkgrntt/go-pco"
)

// serviceTypeCacheTTL matches songCacheTTL's value and reasoning (see
// pco_songs.go): a service type list changes rarely enough that a minute
// of staleness is a non-issue, and this exists purely to stop re-fetching
// it independently on every page that happens to need it.
const serviceTypeCacheTTL = 60 * time.Second

// cachedServiceTypes is one organization's last fetched service type list,
// plus when it was fetched.
type cachedServiceTypes struct {
	serviceTypes []pco.ServiceTypeData
	cachedAt     time.Time
}

var (
	serviceTypeCacheMu sync.Mutex
	serviceTypeCache   = map[string]cachedServiceTypes{}
)

// FetchServiceTypes is the cached, org-scoped entry point for a plain
// GetServiceTypes list. Before this existed, build.go's BuildPlansPage,
// home.go's buildHomeData, and people.go's PositionPreferencesPage each
// called pco.GetServiceTypes independently on their own page loads - cheap
// per call (a service type list is small, no pagination involved, unlike
// the song library FetchActiveSongs guards), but broad in reach across
// three separate pages with zero caching between them. Results are cached
// per organization for serviceTypeCacheTTL, same pattern (and TTL) as
// FetchActiveSongs - a cache hit within the window returns the cached
// slice with no PCO call at all.
//
// A transient failure is never cached (same convention as FetchActiveSongs
// and nav.go's PopulateNavState) - only a successful fetch is stored, so
// one bad PCO response can't pin a stale/empty result in for the rest of
// the TTL window; the next call just retries the live lookup.
func FetchServiceTypes(ctx context.Context, organizationID string) ([]pco.ServiceTypeData, error) {
	now := time.Now()

	serviceTypeCacheMu.Lock()
	entry, ok := serviceTypeCache[organizationID]
	serviceTypeCacheMu.Unlock()
	if ok && songCacheEntryFresh(entry.cachedAt, now, serviceTypeCacheTTL) {
		return entry.serviceTypes, nil
	}

	resp, err := pco.GetServiceTypes(ctx, nil)
	if err != nil {
		return nil, err
	}

	serviceTypeCacheMu.Lock()
	serviceTypeCache[organizationID] = cachedServiceTypes{serviceTypes: resp.Data, cachedAt: now}
	serviceTypeCacheMu.Unlock()

	return resp.Data, nil
}

// InvalidateServiceTypesCache clears organizationID's cached service type
// list, if any. Not called anywhere yet - service types essentially never
// change (they're PCO's own top-level "Sunday Service"/"Youth Service"-
// style groupings, not something a user adds/removes through this app) -
// but exported to match FetchActiveSongs/InvalidateActiveSongsCache's
// shape in case a future caller needs a guaranteed-fresh read.
func InvalidateServiceTypesCache(organizationID string) {
	serviceTypeCacheMu.Lock()
	delete(serviceTypeCache, organizationID)
	serviceTypeCacheMu.Unlock()
}
