package views

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// TestStyleGuideRenders is a smoke test for the design-system reference
// page's several dynamic style="..." attributes (sgColorSwatch/sgTypeRow/
// etc., each built with fmt.Sprintf) - templ sanitizes plain-string style
// values (see safehtml.SanitizeStyleValue) rather than rejecting them, but
// an unsupported value type falls back to a silent placeholder string
// instead of an error, so "it rendered without error" alone wouldn't catch
// that class of mistake.
func TestStyleGuideRenders(t *testing.T) {
	var buf bytes.Buffer
	if err := StyleGuide("Test Person", "Test Church").Render(context.Background(), &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "var(--bg)") {
		t.Error("expected a color swatch referencing var(--bg)")
	}
	if !strings.Contains(out, "font-size: var(--text-3xl)") {
		t.Error("expected a type-scale row for --text-3xl")
	}
	if strings.Contains(out, "zTemplUnsupportedStyleAttributeValue") {
		t.Error("a style attribute value wasn't a type templ's sanitizer recognized")
	}
}
