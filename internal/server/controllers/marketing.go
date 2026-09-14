package controllers

import (
	"fmt"
	"strings"

	"pco-assistant/internal/views"

	"github.com/gofiber/fiber/v2"
)

// MarketingController serves the public marketing pages that sit alongside
// the homepage (Landing, served separately by HomeController.Index) -
// Pricing, HowItWorks, Privacy, and Terms, all pure static content with no
// auth/org dependency at all, unlike Landing which still branches on
// sign-in status. Registered in publicPrefixes (middleware/auth.go) so
// they're reachable with no session, same as /login.
type MarketingController struct{}

func NewMarketingController() *MarketingController {
	return &MarketingController{}
}

func (mc *MarketingController) RegisterRoutes(router fiber.Router) {
	router.Get("/pricing", mc.Pricing)
	router.Get("/how-it-works", mc.HowItWorks)
	router.Get("/privacy", mc.Privacy)
	router.Get("/terms", mc.Terms)
	router.Get("/sitemap.xml", mc.Sitemap)
	router.Get("/robots.txt", mc.Robots)
}

func (mc *MarketingController) Pricing(c *fiber.Ctx) error {
	return render(c, views.Pricing())
}

func (mc *MarketingController) HowItWorks(c *fiber.Ctx) error {
	return render(c, views.HowItWorks())
}

func (mc *MarketingController) Privacy(c *fiber.Ctx) error {
	return render(c, views.Privacy())
}

func (mc *MarketingController) Terms(c *fiber.Ctx) error {
	return render(c, views.Terms())
}

// sitemapPaths is every URL this app wants indexed - the same five public,
// indexable pages marketingMeta (landing.templ) opts in to canonical/Open
// Graph tags for. Nothing behind RequireAuth belongs here; see Layout's own
// noindex meta tag for why those pages opt out instead.
var sitemapPaths = []string{"/", "/pricing", "/how-it-works", "/privacy", "/terms"}

// Sitemap builds "/sitemap.xml" from sitemapPaths and views.MarketingBaseURL
// (the same APP_BASE_URL-derived origin marketingMeta uses) rather than a
// static file, so a domain change is still just the one env var - see
// views.MarketingBaseURL's own doc comment.
func (mc *MarketingController) Sitemap(c *fiber.Ctx) error {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")
	for _, path := range sitemapPaths {
		fmt.Fprintf(&b, "  <url><loc>%s</loc></url>\n", views.MarketingBaseURL+path)
	}
	b.WriteString(`</urlset>`)
	c.Set(fiber.HeaderContentType, "application/xml")
	return c.SendString(b.String())
}

// Robots builds "/robots.txt" the same way Sitemap builds "/sitemap.xml" -
// dynamically off views.MarketingBaseURL rather than a static file under
// web/static/, so the Sitemap directive's absolute URL doesn't become a
// second hardcoded copy of the domain to remember to update. Everything
// behind RequireAuth is already unreachable to a crawler with no session
// (redirected to /login) and already opts out via Layout's noindex meta
// tag, so the explicit Disallow entries below are belt-and-suspenders, not
// the only thing keeping those pages out of an index.
func (mc *MarketingController) Robots(c *fiber.Ctx) error {
	disallow := []string{"/scheduling", "/account", "/billing", "/feedback", "/api"}
	var b strings.Builder
	b.WriteString("User-agent: *\n")
	b.WriteString("Allow: /\n")
	for _, path := range disallow {
		fmt.Fprintf(&b, "Disallow: %s\n", path)
	}
	fmt.Fprintf(&b, "\nSitemap: %s/sitemap.xml\n", views.MarketingBaseURL)
	c.Set(fiber.HeaderContentType, "text/plain")
	return c.SendString(b.String())
}
