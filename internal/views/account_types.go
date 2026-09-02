package views

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
}
