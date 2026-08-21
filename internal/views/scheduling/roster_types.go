package scheduling

// RosterBuilderData gathers everything the Team Builder page (and every
// htmx fragment it swaps after an add/remove action) needs to render - the
// people equivalent of SetBuilderData.
type RosterBuilderData struct {
	ServiceTypeID string
	PlanID        string
	PlanTitle     string
	PlanDates     string
	ErrMsg        string
	// PublishMsg is a status line for the last "Publish to Planning Center"
	// attempt, separate from ErrMsg so a partial publish can show both what
	// succeeded and what didn't - see SetBuilderData.PublishMsg.
	PublishMsg string

	Positions []PositionGroupView
	// Suggestions lists people related (works_well/mentorship) to someone
	// already on the roster, each attached to a specific open position
	// they're eligible for - see SuggestionView.
	Suggestions []PersonSuggestionView
	// ExistingConflicts flags avoid_pairing pairs where both people are
	// already assigned to this roster - see SetBuilderData.ExistingConflicts.
	ExistingConflicts []string

	// UnpublishedCount/PendingDeleteCount drive whether the Publish button
	// renders, same as SetBuilderData.
	UnpublishedCount   int
	PendingDeleteCount int

	// ImportableAssignmentCount is how many people Planning Center already
	// has assigned to this plan's positions that aren't reflected here yet -
	// see SetBuilderData.ImportableSongCount, which this mirrors, and
	// buildRosterBuilderData for why it can't just be read off Positions.
	ImportableAssignmentCount int
}

// PositionGroupView is one open slot - a team + position name, how many are
// needed, who's currently filling it, and (for positions with room left)
// the pool of people eligible to fill it, each annotated with any
// frequency/blockout warning.
type PositionGroupView struct {
	TeamID           string
	TeamName         string
	TeamPositionName string
	Quantity         int
	Assigned         []AssignmentView
	Candidates       []CandidatePersonView
}

// AssignmentView is one person currently filling a position.
type AssignmentView struct {
	ID       int64
	PersonID string
	Name     string
	Status   string
	// Published is true once this assignment has been pushed to Planning
	// Center as a real PlanPerson - see SetSongView.Published.
	Published bool
	// FrequencyWarning/BlockoutWarning are empty when there's nothing to
	// flag - see the controller's frequencyWarning/blockoutWarning.
	FrequencyWarning string
	BlockoutWarning  string
}

// CandidatePersonView is a person eligible for a position but not currently
// assigned to it on this roster - the people equivalent of
// CandidateSongView, scoped to one position rather than the whole
// candidate pool (see buildRosterBuilderData's doc comment for why).
type CandidatePersonView struct {
	PersonID           string
	Name               string
	SchedulePreference string
	FrequencyWarning   string
	BlockoutWarning    string
}

// PersonSuggestionView is one recommended person for a specific open
// position, with every reason they're suggested.
type PersonSuggestionView struct {
	PersonID          string
	Name              string
	TeamID            string
	TeamPositionName  string
	RelationshipLabel string
}
