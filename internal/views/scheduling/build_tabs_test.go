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
	// mouseenter/focus fire before a real click ever would (see
	// tabButtonAttrs' own doc comment) - click stays only as a fallback for
	// devices with no reliable hover/focus-before-click, so it must be
	// present alongside the other two, not instead of them.
	trigger, _ := attrs["hx-trigger"].(string)
	for _, want := range []string{"mouseenter once", "focus once", "click once"} {
		if !strings.Contains(trigger, want) {
			t.Errorf("expected hx-trigger %q to contain %q", trigger, want)
		}
	}
}
