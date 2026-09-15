package auth

import "strings"

// DeviceLabel turns a raw User-Agent header (see ActiveSession.UserAgent)
// into a short human label like "Safari on iPhone" or "Chrome on Windows" -
// just enough for the Account page's "who's signed in" list to tell apart
// two live sessions belonging to the same person, not an exhaustive
// UA-parsing job. Plain substring heuristics rather than a dependency,
// matching this codebase's general "no extra machinery for a simple thing"
// stance (see CLAUDE.md's plain-JS-no-bundler convention for the same
// spirit applied elsewhere).
//
// Browser tokens are checked in an order that avoids the well-known false
// positives baked into real UA strings: Chrome's UA also contains
// "Safari", Edge's also contains "Chrome", Opera's also contains "Chrome",
// and the iOS browser wrappers (CriOS/FxiOS) each also contain "Safari" -
// so the more specific token always has to be checked first, and plain
// "Safari" last, only once nothing more specific matched.
func DeviceLabel(userAgent string) string {
	if strings.TrimSpace(userAgent) == "" {
		return "Unknown device"
	}

	browser := browserLabel(userAgent)
	os := osLabel(userAgent)

	switch {
	case browser != "" && os != "":
		return browser + " on " + os
	case browser != "":
		return browser
	case os != "":
		return os
	default:
		return "browser"
	}
}

func browserLabel(ua string) string {
	switch {
	case strings.Contains(ua, "Edg/") || strings.Contains(ua, "EdgiOS/"):
		return "Edge"
	case strings.Contains(ua, "OPR/"):
		return "Opera"
	case strings.Contains(ua, "CriOS/"):
		return "Chrome"
	case strings.Contains(ua, "FxiOS/"):
		return "Firefox"
	case strings.Contains(ua, "Chrome/"):
		return "Chrome"
	case strings.Contains(ua, "Firefox/"):
		return "Firefox"
	case strings.Contains(ua, "Safari/"):
		return "Safari"
	default:
		return ""
	}
}

func osLabel(ua string) string {
	switch {
	case strings.Contains(ua, "iPhone"):
		return "iPhone"
	case strings.Contains(ua, "iPad"):
		return "iPad"
	case strings.Contains(ua, "Android"):
		return "Android"
	case strings.Contains(ua, "Macintosh"):
		return "Mac"
	case strings.Contains(ua, "Windows"):
		return "Windows"
	case strings.Contains(ua, "CrOS"):
		return "Chromebook"
	case strings.Contains(ua, "Linux"):
		return "Linux"
	default:
		return ""
	}
}
