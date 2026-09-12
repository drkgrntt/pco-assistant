package scheduling

// VocalRangeMatrixData is the whole payload for the vocal-range/key matrix
// page (vocal_range_matrix.templ) - see the controller's
// VocalRangeMatrixPage/buildVocalRangeMatrixData for how it's assembled,
// and docs/plans/not-started/vocal-range-matrix-and-crowdsourced-default-plan.md's
// "The matrix view" section for this feature's own scoping decisions.
// Deliberately scoped to one plan at a time, matching how the Build page
// and roster builder already scope everything: Songs is that plan's own
// set songs (the matrix's columns), People that plan's own roster
// assignments (the rows). A pure read over data buildSetBuilderData
// already produces plus a local roster/person-range read - no new
// storage, and no new PCO calls beyond the one batched
// personNameMapForIDs call this package already makes elsewhere for the
// same purpose.
type VocalRangeMatrixData struct {
	ServiceTypeID string
	PlanID        string
	PlanTitle     string
	PlanDates     string
	ErrMsg        string

	// Songs is this plan's own set songs, in the same order the Songs tab
	// shows them - the matrix's columns.
	Songs []VocalRangeMatrixSong

	// People is this plan's own roster assignments, one row per unique
	// person, sorted alphabetically by display name - the matrix's rows.
	// Includes everyone assigned to any position on the roster, not just
	// vocalists with a stored range on file (see VocalRangeMatrixCell's
	// Unknown verdict for why that's fine to render, rather than a false
	// pass or warning) - "that plan's own roster" is this feature's own
	// literal scoping decision, not a vocalist-only filter.
	People []VocalRangeMatrixPerson

	// Cells[personID][songID] is that pair's verdict - always populated
	// for every person/song combination (see the controller's
	// buildVocalRangeMatrixData), so the template never needs to handle a
	// missing entry as a separate case from VocalRangeMatrixUnknown.
	Cells map[string]map[string]VocalRangeMatrixCell
}

// VocalRangeMatrixSong is one matrix column - a plan's own set song,
// already carrying its effective (transposed) key and practical sung
// range exactly as the Songs tab's own SetSongView shows them (the same
// values, copied across rather than recomputed - see the controller's
// buildVocalRangeMatrixData).
type VocalRangeMatrixSong struct {
	SongID       string
	Title        string
	Key          string
	RangeDisplay string
	// HasRange mirrors SetSongView.VocalRangeHasPlayableNotes - false
	// means this song has no stored range, or an unparseable reference/
	// effective key, in which case every cell in this column is Unknown
	// regardless of what any person's own range says.
	HasRange bool
	// LowPitchClass/LowOctave/HighPitchClass/HighOctave are this song's
	// same transposed range as RangeDisplay, already split into playable
	// ints (mirrors SetSongView.VocalRangeLowPitchClass etc.) - used by
	// the controller's cell classification, not rendered directly.
	LowPitchClass  int
	LowOctave      int
	HighPitchClass int
	HighOctave     int
}

// VocalRangeMatrixPerson is one matrix row - one person assigned to this
// plan's roster (any position, not just vocal ones - see
// VocalRangeMatrixData.People's own doc comment).
type VocalRangeMatrixPerson struct {
	PersonID string
	Name     string
	// RangeDisplay is this person's own stored comfortable range
	// ("A3-D5"), "" when HasRange is false.
	RangeDisplay string
	HasRange     bool
}

// VocalRangeMatrixVerdict is one cell's classification - see
// vocalRangeMatrixCellFor (the controller) and classifyVocalRangeCell
// (sets_builder.go), the exact same fits/tip/mismatch rule the Songs tab's
// own per-song mismatch badge (vocalRangeWarningForSong) already applies
// per vocalist, reused here so this matrix can never disagree with that
// badge about the same person/song pair.
type VocalRangeMatrixVerdict int

const (
	// VocalRangeMatrixUnknown is the zero value - either this song or this
	// person has no usable stored range on file, so there's nothing to
	// compare. Deliberately its own distinct state, not folded into Fits
	// or Mismatch - a blank cell would misleadingly read as either "no
	// problem" or "not checked yet" rather than "no data to check at
	// all," per this feature's own scoping decision.
	VocalRangeMatrixUnknown VocalRangeMatrixVerdict = iota
	// VocalRangeMatrixFits is a real comparison that came back clean - the
	// song's range, as written, sits fully inside this person's own.
	VocalRangeMatrixFits
	// VocalRangeMatrixTip mirrors SetSongView.VocalRangeWarningIsTip - the
	// song's range doesn't fit as written, but shifting it a full octave
	// up or down would bring it fully inside this person's own (see
	// vocalRangeFitsShifted, sets_builder.go).
	VocalRangeMatrixTip
	// VocalRangeMatrixMismatch mirrors a hard mismatch on the Songs tab's
	// own badge - doesn't fit as written, and no octave shift fixes it
	// either.
	VocalRangeMatrixMismatch
)

// VocalRangeMatrixCell is one grid cell - Message is a one-sentence
// explanation for Tip/Mismatch ("" for Fits/Unknown, nothing to explain
// either way), shown in the same click-to-expand Disclosure popover the
// Songs tab's own badge uses (see vocal_range_matrix.templ's
// vocalRangeMatrixCell).
type VocalRangeMatrixCell struct {
	Verdict VocalRangeMatrixVerdict
	Message string
}
