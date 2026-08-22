package views

import (
	"strconv"
	"time"
)

// AssetVersion is a cache-busting query param appended to every static
// CSS/JS URL in Layout. Computed once, when the process starts - a fresh
// deploy always restarts the process (see Dockerfile/docker-compose.yml),
// so every deploy gets a brand-new value and therefore a brand-new URL for
// every asset, which both browsers and Cloudflare's edge cache can only
// ever see as a fresh miss. That's what actually fixes staleness, not the
// TTL on any one cached response: the live site sat behind Cloudflare
// serving a ~70-minute-stale app.css after a CSS-only deploy (the HTML
// referencing it was fresh, since it's never cached, but the plain
// /static/css/app.css URL was still the one Cloudflare had on file from
// before the change) - this makes that class of bug impossible instead of
// something to remember to purge by hand after every deploy that touches
// CSS/JS.
var AssetVersion = strconv.FormatInt(time.Now().Unix(), 10)
