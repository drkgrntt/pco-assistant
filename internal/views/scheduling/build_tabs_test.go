package scheduling

import (
	"strings"
	"testing"
)

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
	attrs := tabPanelLazyAttrs("", "team")
	if attrs != nil {
		t.Errorf("expected nil attrs for an empty lazyURL, got %v", attrs)
	}
}

func TestTabPanelLazyAttrsSetWhenURLPresent(t *testing.T) {
	attrs := tabPanelLazyAttrs("/scheduling/build/st1/pl1/team-tab", "team")

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

	trigger, _ := attrs["hx-trigger"].(string)
	for _, want := range []string{"load once", "click once from:#build-tab-button-team"} {
		if !strings.Contains(trigger, want) {
			t.Errorf("expected hx-trigger %q to contain %q", trigger, want)
		}
	}
}

func TestLazyTabTriggerScopesClickToItsOwnButton(t *testing.T) {
	songs := lazyTabTrigger("songs")
	team := lazyTabTrigger("team")

	if !strings.Contains(songs, "from:#build-tab-button-songs") {
		t.Errorf("expected songs' trigger to scope click to its own button, got %q", songs)
	}
	if !strings.Contains(team, "from:#build-tab-button-team") {
		t.Errorf("expected team's trigger to scope click to its own button, got %q", team)
	}
}
