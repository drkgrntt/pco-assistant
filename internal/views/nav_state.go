package views

import "context"

// navCtxKey backs the two nav-visibility flags middleware.PopulateNavState
// computes once per request and Layout/Home read back - see NavSuggestionsPendingKey/
// NavSongInfoNeedsSyncKey. A small unexported struct type (not a plain
// string) so these keys can never collide with anything else stashed in
// the same context.Context, the standard Go context-key idiom.
//
// Values travel from middleware to here via Fiber's c.Locals, not a
// function parameter threaded through every one of this app's ~20
// page-view functions: Fiber's c.Locals bridges into the context.Context
// a templ component's Render(ctx, w) receives (fasthttp.RequestCtx.Value
// reads the same user-value store Locals writes to - confirmed directly
// against this app's actual Fiber version, not assumed), so Layout can
// just read ctx.Value(NavSuggestionsPendingKey) instead. The keys are
// exported so middleware (a different package) can write to them with
// c.Locals(views.NavSuggestionsPendingKey, ...) using the exact same key
// value; the read-side helpers below stay unexported since only this
// package's own templates need them.
type navCtxKey struct{ name string }

var (
	// NavSuggestionsPendingKey is true when this org has at least one
	// unreviewed Song Tag Suggestions group.
	NavSuggestionsPendingKey = navCtxKey{"nav-suggestions-pending"}
	// NavSongInfoNeedsSyncKey is true when at least one of this org's
	// songs doesn't have cached Song Info yet.
	NavSongInfoNeedsSyncKey = navCtxKey{"nav-song-info-needs-sync"}
)

// navSuggestionsPending/navSongInfoNeedsSync default to false (hide the
// nudge) whenever middleware.PopulateNavState hasn't run or came back
// empty for any reason - same fail-safe direction as the rest of this
// app's nav: only show a nudge when there's a confirmed reason to.
func navSuggestionsPending(ctx context.Context) bool {
	b, _ := ctx.Value(NavSuggestionsPendingKey).(bool)
	return b
}

func navSongInfoNeedsSync(ctx context.Context) bool {
	b, _ := ctx.Value(NavSongInfoNeedsSyncKey).(bool)
	return b
}
