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
}

// buildAccountData resolves everything the page needs except the two
// cache-action messages (each handler sets those on its own render, same
// pattern as BillingController's ErrMsg handling).
func (ac *AccountController) buildAccountData(c *fiber.Ctx) views.AccountData {
	return views.AccountData{
		OrganizationID: middleware.CurrentOrganizationID(c),
		Billing:        buildBillingData(c, ac.authStore),
	}
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
// SyncSongInfo's own re-render.
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
