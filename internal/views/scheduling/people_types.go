package scheduling

// PositionOption is one selectable position within a PositionGroup's team,
// for the position preferences form's dropdown.
type PositionOption struct {
	TeamID           string
	TeamPositionID   string
	TeamPositionName string
}

// PositionGroup is one team's positions, grouped for the preferences
// dropdown's <optgroup> - built server-side (see fetchTeamPositions) so the
// template doesn't need to re-derive grouping from a flat list.
type PositionGroup struct {
	TeamID    string
	TeamName  string
	Positions []PositionOption
}

// PreferenceRow is one existing PersonTeamPositionAssignment - a person
// eligible for a position, with their serving-frequency preference.
type PreferenceRow struct {
	AssignmentID       string
	PersonID           string
	PersonName         string
	SchedulePreference string
}

// SchedulePreferenceOption is one entry of the schedule_preference dropdown -
// PCO's verbatim value paired with a display label.
type SchedulePreferenceOption struct {
	Value string
	Label string
}
