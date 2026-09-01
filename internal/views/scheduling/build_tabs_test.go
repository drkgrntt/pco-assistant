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

func TestTabButtonAttrsAlwaysSetsOnclick(t *testing.T) {
	attrs := tabButtonAttrs(BuildTab{ID: "songs"})
	if attrs["onclick"] != "toggleBuildTab('songs')" {
		t.Errorf("expected onclick to toggle the songs tab, got %v", attrs["onclick"])
	}
}

func TestTabButtonAttrsOmitsLazyFetchWhenLazyURLEmpty(t *testing.T) {
	attrs := tabButtonAttrs(BuildTab{ID: "songs"})
	for _, key := range []string{"hx-get", "hx-trigger", "hx-target", "hx-swap"} {
		if _, ok := attrs[key]; ok {
			t.Errorf("expected no %s attribute for a tab with no LazyURL, got %v", key, attrs[key])
		}
	}
}

func TestTabButtonAttrsAddsLazyFetchWhenLazyURLSet(t *testing.T) {
	attrs := tabButtonAttrs(BuildTab{ID: "team", LazyURL: "/scheduling/build/st1/pl1/team-tab"})

	if attrs["hx-get"] != "/scheduling/build/st1/pl1/team-tab" {
		t.Errorf("expected hx-get to be the LazyURL, got %v", attrs["hx-get"])
	}
	if attrs["hx-target"] != "#build-tab-team" {
		t.Errorf("expected hx-target to point at the team tab panel, got %v", attrs["hx-target"])
	}
	// load fires the fetch immediately on page render, giving the deferred
	// tab the earliest possible head start (see lazyTabTrigger's doc
	// comment); click stays as a same-tab retry in case that first attempt
	// failed. mouseenter/focus are deliberately absent - once "load" always
	// fires the fetch immediately, they never get a chance to do anything
	// load hasn't already started, so they'd only ever add a redundant GET.
	trigger, _ := attrs["hx-trigger"].(string)
	for _, want := range []string{"load once", "click once"} {
		if !strings.Contains(trigger, want) {
			t.Errorf("expected hx-trigger %q to contain %q", trigger, want)
		}
	}
	for _, unwanted := range []string{"mouseenter", "focus"} {
		if strings.Contains(trigger, unwanted) {
			t.Errorf("expected hx-trigger %q not to contain %q - load already covers it", trigger, unwanted)
		}
	}
}
