package scheduling

import "testing"

func TestLazyTabURLEagerReturnsEmpty(t *testing.T) {
	if got := lazyTabURL(true, "st1", "pl1", "songs"); got != "" {
		t.Errorf("expected empty URL for an eager tab, got %q", got)
	}
}

func TestLazyTabURLDeferredBuildsURL(t *testing.T) {
	got := lazyTabURL(false, "st1", "pl1", "songs")
	want := "/scheduling/build/st1/pl1/songs-tab"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestTabButtonAttrsOnlySetsOnclick(t *testing.T) {
	attrs := tabButtonAttrs(BuildTab{ID: "songs"})
	if attrs["onclick"] != "toggleBuildTab('songs')" {
		t.Errorf("expected onclick to toggle the songs tab, got %v", attrs["onclick"])
	}
	// The tab button must never carry hx-get/hx-trigger itself - see
	// tabPanelLazyAttrs' doc comment for the bug that caused (the app-wide
	// htmx-request feedback disabling the button for as long as the
	// load-triggered fetch was in flight, since it can't tell that "load"
	// apart from a real click).
	for _, key := range []string{"hx-get", "hx-trigger", "hx-target", "hx-swap"} {
		if _, ok := attrs[key]; ok {
			t.Errorf("expected no %s attribute on the tab button, got %v", key, attrs[key])
		}
	}
}

func TestTabPanelLazyAttrsNilWhenURLEmpty(t *testing.T) {
	attrs := tabPanelLazyAttrs("")
	if attrs != nil {
		t.Errorf("expected nil attrs for an empty lazyURL, got %v", attrs)
	}
}

func TestTabPanelLazyAttrsSetWhenURLPresent(t *testing.T) {
	attrs := tabPanelLazyAttrs("/scheduling/build/st1/pl1/team-tab")

	if attrs["hx-get"] != "/scheduling/build/st1/pl1/team-tab" {
		t.Errorf("expected hx-get to be the lazyURL, got %v", attrs["hx-get"])
	}
	if attrs["hx-swap"] != "innerHTML" {
		t.Errorf("expected hx-swap innerHTML, got %v", attrs["hx-swap"])
	}
	// No hx-target: the panel div hosts hx-get itself now, so the default
	// target (itself) is already correct - see tabPanelLazyAttrs' doc
	// comment.
	if _, ok := attrs["hx-target"]; ok {
		t.Errorf("expected no hx-target now that hx-get lives on the panel itself, got %v", attrs["hx-target"])
	}
	// Just "load once" - not a second "click" clause. htmx tracks "once"
	// per trigger clause, not shared across them, so a second clause
	// doesn't mean "only if load didn't already fire" - it fires on its
	// own regardless, which used to re-run this fetch on every first
	// click even after load had already finished. See lazyTabTrigger's own
	// doc comment for the full story, including the state-corruption bug
	// that redundant response caused.
	if attrs["hx-trigger"] != lazyTabTrigger {
		t.Errorf("expected hx-trigger %q, got %v", lazyTabTrigger, attrs["hx-trigger"])
	}
}
