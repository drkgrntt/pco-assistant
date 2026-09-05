package views

import "context"

// navCtxKey backs the nav-visibility flag middleware.PopulateNavState
// computes once per request and Layout/Home read back - see
// NavSuggestionsPendingKey. A small unexported struct type (not a plain
// string) so this key can never collide with anything else stashed in
// the same context.Context, the standard Go context-key idiom.
//
// Values travel from middleware to here via Fiber's c.Locals, not a
// function parameter threaded through every one of this app's ~20
// page-view functions: Fiber's c.Locals bridges into the context.Context
// a templ component's Render(ctx, w) receives (fasthttp.RequestCtx.Value
// reads the same user-value store Locals writes to - confirmed directly
// against this app's actual Fiber version, not assumed), so Layout can
// just read ctx.Value(NavSuggestionsPendingKey) instead. The key is
// exported so middleware (a different package) can write to it with
// c.Locals(views.NavSuggestionsPendingKey, ...) using the exact same key
// value; the read-side helper below stays unexported since only this
// package's own templates need it.
//
// Used to carry a second flag, NavSongInfoNeedsSyncKey (whether any of an
// org's songs lacked cached Song Info) - removed along with the Song Info
// page/button it drove a nag link to, once a cache miss started syncing
// itself in the background instead of needing anyone to notice and click
// something (see docs/pco-call-efficiency-rollout-plan.md's Phase 2).
type navCtxKey struct{ name string }

// NavSuggestionsPendingKey is true when this org has at least one
// unreviewed Song Tag Suggestions group.
var NavSuggestionsPendingKey = navCtxKey{"nav-suggestions-pending"}

// navSuggestionsPending defaults to false (hide the nudge) whenever
// middleware.PopulateNavState hasn't run or came back empty for any reason
// - same fail-safe direction as the rest of this app's nav: only show a
// nudge when there's a confirmed reason to.
func navSuggestionsPending(ctx context.Context) bool {
	b, _ := ctx.Value(NavSuggestionsPendingKey).(bool)
	return b
}
