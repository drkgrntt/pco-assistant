package controllers

import (
	"pco-assistant/internal/auth"
	"pco-assistant/internal/scheduling"
	"pco-assistant/internal/server/middleware"
	"pco-assistant/internal/views"

	"github.com/gofiber/fiber/v2"
)

// AccountController serves the one persistent /account page - account/
// organization identity, the same billing section /billing itself renders
// (views.BillingBody, via the shared buildBillingData helper in
// billing.go), and the "Data & Caches" section's two cache-clear actions.
// See layout.templ's site-actions doc comment for why this replaced
// /billing as the nav link - /billing's own routes/page are untouched and
// still work for anyone who navigates there directly or is mid-checkout.
// No *billing.Client here: BillingBody's checkout/portal forms post
// straight to /billing/checkout and /billing/portal (BillingController's
// own routes), not through this controller.
type AccountController struct {
	authStore       *auth.Store
	schedulingStore *scheduling.Store
}

func NewAccountController(authStore *auth.Store, schedulingStore *scheduling.Store) *AccountController {
	return &AccountController{authStore: authStore, schedulingStore: schedulingStore}
}

func (ac *AccountController) RegisterRoutes(router fiber.Router) {
	group := router.Group("/account")

	group.Get("/", ac.AccountPage)
	group.Post("/cache/song-info", ac.ClearSongInfoCache)
	group.Post("/cache/person-data", ac.ClearPersonDataCache)
	group.Post("/sessions/:sessionID/sign-out", ac.SignOutSession)
}

// buildAccountData resolves everything the page needs except the
// cache-action and sign-out-action messages (each handler sets those on
// its own render, same pattern as BillingController's ErrMsg handling).
func (ac *AccountController) buildAccountData(c *fiber.Ctx) views.AccountData {
	orgID := middleware.CurrentOrganizationID(c)

	data := views.AccountData{
		OrganizationID: orgID,
		Billing:        buildBillingData(c, ac.authStore),
	}

	// Best-effort, same tolerance buildBillingData already gives
	// SeatCounts - a failure here just means the "who's signed in" list
	// doesn't show, not a broken page.
	if sessions, err := ac.authStore.ListActiveSessions(c.Context(), orgID); err != nil {
		data.SessionsErr = "loading who's signed in: " + err.Error()
	} else {
		for _, sess := range sessions {
			data.Sessions = append(data.Sessions, views.AccountSession{
				SessionID:  sess.ID,
				PersonID:   sess.PersonID,
				PersonName: sess.PersonName,
				CreatedAt:  sess.CreatedAt,
				ExpiresAt:  sess.ExpiresAt,
			})
		}
	}

	return data
}

func (ac *AccountController) AccountPage(c *fiber.Ctx) error {
	_, personName := middleware.CurrentPerson(c)
	orgName := middleware.CurrentOrganization(c)

	data := ac.buildAccountData(c)
	return render(c, views.Account(personName, orgName, data))
}

// ClearSongInfoCache deletes every cached song arrangement row for this
// org (scheduling.Store.ClearSongArrangementCache) - see that method's
// doc comment for why this is non-destructive. Re-renders just the cache
// fragment (id="account-cache-body"), same htmx-target pattern as
// ClearPersonDataCache's own re-render below. The wiped cache refills
// itself automatically, in the background, the next time a set builder
// render touches one of these songs (triggerSongInfoQueueSync,
// song_arrangement_cache.go) - there's no separate "resync now" action
// here on purpose, see that function's own doc comment.
func (ac *AccountController) ClearSongInfoCache(c *fiber.Ctx) error {
	ctx := middleware.RequestContext(c)
	orgID := middleware.CurrentOrganizationID(c)

	data := ac.buildAccountData(c)
	if err := ac.schedulingStore.ClearSongArrangementCache(ctx, orgID); err != nil {
		data.SongInfoCacheErr = "clearing song info cache: " + err.Error()
		return render(c, views.AccountCacheBody(data))
	}

	data.SongInfoCacheMsg = "Song info cache cleared - it'll be re-collected from Planning Center as needed."
	return render(c, views.AccountCacheBody(data))
}

// ClearPersonDataCache deletes every cached person-data-cache row (person
// blockouts, all-teams plan history, per-team plan history, and each of
// their sync markers) for this org - see
// scheduling.Store.ClearPersonDataCache's doc comment for why this is
// non-destructive.
func (ac *AccountController) ClearPersonDataCache(c *fiber.Ctx) error {
	ctx := middleware.RequestContext(c)
	orgID := middleware.CurrentOrganizationID(c)

	data := ac.buildAccountData(c)
	if err := ac.schedulingStore.ClearPersonDataCache(ctx, orgID); err != nil {
		data.PersonDataCacheErr = "clearing person data cache: " + err.Error()
		return render(c, views.AccountCacheBody(data))
	}

	data.PersonDataCacheMsg = "Person data cache cleared - it'll be re-collected from Planning Center as needed."
	return render(c, views.AccountCacheBody(data))
}

// SignOutSession ends exactly the one live session named by the
// :sessionID param (auth.Store.DeleteSessionByID) - the "Sign out" action
// on one row of the Account page's "who's signed in" list. Deliberately
// keyed off the session's own id rather than PersonID: a person signed in
// on two devices renders two rows sharing one PersonID, and the previous
// person-keyed version (auth.Store.DeleteSessionsForPerson, since removed)
// deleted *every* session that person had no matter which row's button
// was clicked - see AccountSession's doc comment for the same bug from the
// view-model side. Immediately frees a seat if this was the only session
// that person had (see auth.SeatCounts), rather than waiting for it to age
// out.
//
// No org-membership check on the id itself (DeleteSessionByID isn't
// org-scoped) - it doesn't need one: the id is a SHA-256 hash of a random
// 32-byte token (see hashToken), so the only realistic way to have one at
// all is to already be looking at this org's own "who's signed in" table,
// which RequireActiveOrganization has already gated. Beyond that, no
// separate authorization check: every signed-in person for an org is
// already, by this product's own definition, a full admin (see README's
// Pricing section), so any of them can sign any other session out. Re-
// renders just the sessions fragment (id="account-sessions-body"), same
// htmx-target pattern as the two cache-clear handlers above.
func (ac *AccountController) SignOutSession(c *fiber.Ctx) error {
	sessionID := c.Params("sessionID")

	if err := ac.authStore.DeleteSessionByID(c.Context(), sessionID); err != nil {
		data := ac.buildAccountData(c)
		data.SignOutErr = "signing that session out: " + err.Error()
		return render(c, views.AccountSessionsBody(data))
	}

	data := ac.buildAccountData(c)
	data.SignOutMsg = "Signed out."
	return render(c, views.AccountSessionsBody(data))
}
