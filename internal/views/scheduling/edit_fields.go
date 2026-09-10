package scheduling

import (
	"encoding/json"
	"strconv"
)

// editFieldsAttr JSON-encodes a setup-page row's current values for its
// Edit button's data-edit-fields attribute (see editRow in
// web/static/js/scheduling.js, which reads it back and writes each value into
// the matching named form field). templ escapes the attribute value for
// us; this only needs to produce valid JSON. Encoding a map[string]string
// can't actually fail, but a swallowed "{}" beats panicking a page render
// over a cosmetic attribute.
func editFieldsAttr(fields map[string]string) string {
	b, err := json.Marshal(fields)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// editPerson is one person-picker's edit-time value: the id its hidden
// input needs plus the display name its visible search box should show
// (search boxes hold a name, not an id - see PersonPickerField).
type editPerson struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// editPeopleAttr JSON-encodes a row's person-picker values for its Edit
// button's data-edit-people attribute, keyed by the same suffix each
// picker already uses to disambiguate itself on the page ("" for a page's
// only picker, "a"/"b"/"pref" for pages with more than one - see
// PersonPickerField/idSuffix).
func editPeopleAttr(people map[string]editPerson) string {
	b, err := json.Marshal(people)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// displayNameOrID resolves id through names, falling back to the raw id
// when it's missing (the same fallback every table in this package already
// shows in its own cells) - for building an Edit button's editPerson value,
// so the search box an edit fills in shows the same name (or id) the table
// row itself is showing, not a blank.
func displayNameOrID(names map[string]string, id string) string {
	if name, ok := names[id]; ok {
		return name
	}
	return id
}

// displayLabelOrID is displayNameOrID's int64-keyed counterpart -
// song_context_types uses a surrogate BIGSERIAL id as its join key (unlike
// song_role_types' human-legible string code), so resolving a context
// assignment's stored id back to its label needs an int64 lookup instead of
// a string one. Falls back to the id's decimal string form, same "raw code
// if it doesn't resolve" convention as displayNameOrID.
func displayLabelOrID(labels map[int64]string, id int64) string {
	if label, ok := labels[id]; ok {
		return label
	}
	return strconv.FormatInt(id, 10)
}
