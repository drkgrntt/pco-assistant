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
// something (see docs/plans/shipped/pco-call-efficiency-rollout-plan.md's Phase 2).
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

// PCORateLimitedKey is true when at least one PCO call made during this
// same request hit a 429 - see
// docs/plans/shipped/pco-rate-limit-user-visibility-plan.md's Phase 1.
// Reuses navCtxKey's type (it's already a generic "unexported struct key"
// wrapper, not nav-specific despite the type name) rather than inventing a
// second key type for one more value.
//
// Unlike NavSuggestionsPendingKey and every other flag in this file, this
// one is populated by controllers/render.go, not by
// middleware.PopulateNavState (or any other pre-handler middleware): a
// 429 can only be known about *after* the handler's own PCO calls have
// run, so there's nothing for a pre-handler middleware to check yet. It's
// also read-once at the source (middleware.HadPCORateLimit consumes the
// mark it reads), so a given render sees it at most once, by design - see
// that function's own doc comment.
var PCORateLimitedKey = navCtxKey{"pco-rate-limited"}

// pcoRateLimited defaults to false (hide the banner) whenever render.go
// hasn't set it or it came back false - same fail-safe direction as
// navSuggestionsPending above: only show the banner when there's a
// confirmed 429 to report.
func pcoRateLimited(ctx context.Context) bool {
	b, _ := ctx.Value(PCORateLimitedKey).(bool)
	return b
}
