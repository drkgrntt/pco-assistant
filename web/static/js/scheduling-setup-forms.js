// scheduling-setup-forms.js - generic create/edit form helpers shared by
// the 8 scheduling/*.templ setup pages (song relationships, song roles,
// song contexts, song tags, song/person preferences, person relationships,
// position preferences, vocal range): the person picker's selectPerson,
// hidden-field value handling (setHiddenFieldValue), resetSetupForm,
// applyEditFields/editRow/cancelSetupEdit, and the Build page's "Set song
// vocal range" quick-add form's prefill-from-selected-song
// (fillVocalRangeFromSongSelect). Split out of the former single
// scheduling.js - see scheduling-global.js's header for the same note.

// selectPerson fills a person picker's hidden person_id input and visible
// search box from a clicked search result's data attributes (see
// PersonSearchResults in internal/views/scheduling/person_picker.templ),
// then clears that picker's results list. data-suffix identifies which
// picker this result belongs to ("" for the original single-picker ids, "a"
// / "b" etc. for pages with more than one picker on the same form - e.g.
// person relationships' "Person A"/"Person B") - see idSuffix in
// person_picker.templ for the matching id scheme.
function selectPerson(el) {
  var suffix = el.dataset.suffix ? "-" + el.dataset.suffix : "";
  setHiddenFieldValue(document.getElementById("person-id-input" + suffix), el.dataset.personId);
  document.getElementById("person-search-input" + suffix).value = el.dataset.personName;
  // Deferred, not synchronous: this handler runs during the click event's
  // target phase (invoked by the clicked .search-result div's own
  // onclick), before the event finishes bubbling up to document.
  // Clearing this container's innerHTML synchronously would detach the
  // just-clicked element (the event's own target) from the DOM right
  // then, so by the time the click reaches document's own listener
  // further down this file, event.target.closest(...) can no longer find
  // ancestors that were genuinely there when the click happened (e.g. a
  // picker embedded inside a .build-detail-card, like the song leader
  // picker) - closest() on a detached node returns null, so that listener
  // wrongly concludes the click landed outside the card and closes it.
  // setTimeout(fn, 0) pushes this mutation to a fresh task, after the
  // current event has fully finished bubbling, so every listener sees the
  // real DOM as it was at click time. The value assignments above stay
  // synchronous - only this container's clear needs deferring.
  var resultsContainer = document.getElementById("person-search-results" + suffix);
  setTimeout(function () {
    resultsContainer.innerHTML = "";
  }, 0);
}

// Any further edit to a person search box invalidates whatever was
// previously selected in that same picker - without this, editing the text
// after clicking a result would leave that picker's hidden id input still
// pointing at the old (now-mismatched) person while the visible box shows
// different, unconfirmed text. Setting .value from selectPerson() above
// doesn't fire "input", so this doesn't clear the selection it just made.
// Matches "person-search-input" or "person-search-input-<suffix>" and clears
// the correspondingly-suffixed hidden input.
document.addEventListener("input", function (event) {
  var id = event.target.id || "";
  if (id.indexOf("person-search-input") !== 0) {
    return;
  }
  var hidden = document.getElementById("person-id-input" + id.slice("person-search-input".length));
  if (hidden) {
    hidden.value = "";
  }
});

// setHiddenFieldValue sets a hidden <input>'s value via JS while preserving
// its true original default (in data-original-value, captured the first
// time this runs) for resetSetupForm to restore later.
//
// Per the HTML spec's value-sanitization table, hidden inputs (along with
// submit/reset/button) have value IDL attribute mode "default": .value IS
// the "value" content attribute - the exact same one .defaultValue reads.
// So a plain `el.value = x` assignment on one of these doesn't just set the
// current value, it also silently rewrites what a later native
// form.reset() treats as "the default" to reset back to (verified against
// the real HTML spec algorithm and confirmed live: MDN/WHATWG's "default"
// value mode - true in every engine, not a jsdom quirk). Every hidden field
// this app fills via JS - the shared "id" field editRow populates (see
// parseFormID's create/update convention) and a PersonPickerField's hidden
// person-id input (editRow's own people-fill loop, selectPerson) - hits
// this exact trap: without capturing/restoring the pre-JS default
// ourselves, form.reset() "resets" the field right back to whatever was
// just set instead of its real original (usually empty) value, so the very
// next create submit on that same form silently reuses stale data (e.g.
// updates the row that was last edited instead of creating a new one).
// Regular (non-hidden) fields are unaffected - text/select inputs use
// value mode "value", where .value and .defaultValue are genuinely
// separate, so native reset() already handles them correctly.
function setHiddenFieldValue(el, value) {
  if (el.dataset.originalValue === undefined) {
    el.dataset.originalValue = el.value;
  }
  el.value = value;
}

// Shared reset for the setup pages' create/edit forms (song relationships,
// song/person preferences, song roles, person relationships) - restores a
// form to its "add a new one" state: native reset() for ordinary fields,
// plus explicit restoration of every hidden field's true original default
// (see setHiddenFieldValue - native reset() can't do this part itself),
// including person pickers' hidden id input and the "id" field that
// switches the next submit from update back to create, plus the submit
// button/Cancel affordance out of "editing" mode. Called both after a
// successful create/update submit (hx-on::after-request below) and from
// the Cancel button (cancelSetupEdit).
function resetSetupForm(form) {
  form.reset();
  Array.prototype.forEach.call(form.querySelectorAll("input[type=hidden]"), function (el) {
    if (el.dataset.originalValue !== undefined) {
      el.value = el.dataset.originalValue;
    }
  });
  form.classList.remove("editing");
  var submitBtn = form.querySelector("button[type=submit]");
  if (submitBtn && submitBtn.dataset.createLabel) {
    submitBtn.textContent = submitBtn.dataset.createLabel;
  }
  var cancelBtn = form.querySelector(".cancel-edit");
  if (cancelBtn) {
    cancelBtn.hidden = true;
  }
}

// applyEditFields writes a data-edit-fields-style JSON object (plain
// form-field name -> value, see editFieldsAttr) into form's matching named
// fields - the field-filling core editRow below uses, pulled out so
// fillVocalRangeFromSongSelect (a <select>-driven prefill, not a button
// click) can reuse the exact same fill logic instead of duplicating it.
function applyEditFields(form, fields) {
  Object.keys(fields).forEach(function (name) {
    var el = form.elements.namedItem(name);
    if (el) {
      if (el.type === "hidden") {
        setHiddenFieldValue(el, fields[name]);
      } else {
        el.value = fields[name];
      }
    }
  });
}

// editRow fills a setup page's create/edit form from a table row's Edit
// button and switches it into "editing" mode, so the next submit updates
// this row (via its now-populated hidden "id" field) instead of creating a
// new one. data-edit-fields is a JSON object of plain form-field name ->
// value (see editFieldsAttr); data-edit-people is a JSON object of person-
// picker suffix -> {id, name} (see editPeopleAttr, PersonPickerField) for
// pages that search people rather than using a plain <select>.
function editRow(button) {
  var form = document.getElementById(button.dataset.editForm);
  if (!form) {
    return;
  }

  applyEditFields(form, JSON.parse(button.dataset.editFields || "{}"));

  var people = JSON.parse(button.dataset.editPeople || "{}");
  Object.keys(people).forEach(function (suffix) {
    var idSuffix = suffix ? "-" + suffix : "";
    var idInput = document.getElementById("person-id-input" + idSuffix);
    var searchInput = document.getElementById("person-search-input" + idSuffix);
    if (idInput) {
      setHiddenFieldValue(idInput, people[suffix].id);
    }
    if (searchInput) {
      searchInput.value = people[suffix].name;
    }
  });

  form.classList.add("editing");
  var submitBtn = form.querySelector("button[type=submit]");
  if (submitBtn) {
    if (!submitBtn.dataset.createLabel) {
      submitBtn.dataset.createLabel = submitBtn.textContent;
    }
    submitBtn.textContent = "Update";
  }
  var cancelBtn = form.querySelector(".cancel-edit");
  if (cancelBtn) {
    cancelBtn.hidden = false;
  }

  // The Song Tags page's own add-tag form (song_tags.templ) sits collapsed
  // inside a <details> to stay compact - editing an existing tag needs it
  // open, since a closed <details> hides the very fields editRow just
  // filled in. Every other setup page's form isn't wrapped in one, so
  // form.closest("details") is simply null there and this is a no-op.
  var details = form.closest("details");
  if (details) {
    details.open = true;
  }

  form.scrollIntoView({ behavior: "smooth", block: "start" });
}

function cancelSetupEdit(button) {
  var form = button.closest("form");
  resetSetupForm(form);
  var details = form.closest("details");
  if (details) {
    details.open = false;
  }
}

// fillVocalRangeFromSongSelect pre-fills the Build page's quick-add "Set
// song vocal range" form from whichever song its own picker just selected
// - docs/issues.md: "fill in the form with the existing data and make the
// save an update if applicable" (scheduling.SetSongVocalRange already
// upserts by song id regardless of what the form shows - this just makes
// the form honestly reflect that a song already has a range on file,
// mirroring editRow's fill-and-relabel behavior above but triggered by a
// <select>'s change instead of an Edit button's click). The selected
// <option>'s own data-edit-fields (same JSON shape/helper editRow already
// reads, editFieldsAttr/vocalRangeFormFields) carries reference_key/
// lowest_note/highest_note when that song has a range on file - an empty
// object otherwise, in which case every field is cleared back to blank
// instead of keeping some previously-selected song's stale values.
function fillVocalRangeFromSongSelect(select) {
  var form = select.closest("form");
  if (!form) {
    return;
  }

  var opt = select.selectedOptions[0];
  var fields = opt ? JSON.parse(opt.dataset.editFields || "{}") : {};
  applyEditFields(form, {
    reference_key: fields.reference_key || "",
    lowest_note: fields.lowest_note || "",
    highest_note: fields.highest_note || "",
  });

  var submitBtn = form.querySelector("button[type=submit]");
  if (submitBtn) {
    if (!submitBtn.dataset.createLabel) {
      submitBtn.dataset.createLabel = submitBtn.textContent;
    }
    submitBtn.textContent = fields.reference_key
      ? "Update range"
      : submitBtn.dataset.createLabel;
  }
}
