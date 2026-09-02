package views

import "time"

// AccountSession is a view-local mirror of auth.ActiveSession - keeps this
// package from importing internal/auth types directly, the same boundary
// BillingData already keeps between itself and auth.Organization (see
// BillingData's own doc comment / buildBillingData, which copies fields
// across rather than embedding the auth type).
type AccountSession struct {
	PersonID, PersonName string
	CreatedAt, ExpiresAt time.Time
}

// AccountData is the Account page's view model - identity info plus the
// same BillingData the standalone /billing page uses (see
// AccountController.buildAccountData/BillingController.buildBillingData,
// both built by the shared buildBillingData helper in
// internal/server/controllers/billing.go), plus the Data & Caches
// section's own per-action status.
type AccountData struct {
	// OrganizationID is the real Planning Center organization id - shown
	// here (unlike anywhere else in the nav) as a stable reference for
	// support/debugging, since OrganizationName can change if the org
	// renames itself in Planning Center.
	OrganizationID string

	Billing BillingData

	// SongInfoCacheMsg/SongInfoCacheErr report the result of the last
	// POST /account/cache/song-info on this render - mirrors
	// scheduling.SongInfoData's SyncMsg/ErrMsg fields/doc-comment style.
	SongInfoCacheMsg string
	SongInfoCacheErr string

	// PersonDataCacheMsg/PersonDataCacheErr are the person-data cache
	// clear's counterpart to the two fields above.
	PersonDataCacheMsg string
	PersonDataCacheErr string

	// Sessions lists every currently signed-in person for this org (see
	// auth.Store.ListActiveSessions) - the Account page's "who's signed
	// in" section, most recent first.
	Sessions []AccountSession
	// SessionsErr reports a failure loading Sessions - best-effort, same
	// tolerance buildBillingData already gives SeatCounts: a failure here
	// just leaves the list empty rather than breaking the whole page.
	SessionsErr string

	// SignOutMsg/SignOutErr report the result of the last
	// POST /account/sessions/:personID/sign-out on this render - mirrors
	// SongInfoCacheMsg/SongInfoCacheErr's style.
	SignOutMsg string
	SignOutErr string
}
