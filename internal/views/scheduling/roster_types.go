package scheduling

import "pco-assistant/internal/scheduling"

// RosterBuilderData gathers everything the Team Builder page (and every
// htmx fragment it swaps after an add/remove action) needs to render - the
// people equivalent of SetBuilderData.
type RosterBuilderData struct {
	ServiceTypeID string
	PlanID        string
	PlanTitle     string
	PlanDates     string
	ErrMsg        string
	// ReadOnly - see SetBuilderData.ReadOnly, which this mirrors exactly.
	ReadOnly bool
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

	// OpenPositionCount is how many of Positions still have room left (len
	// (Assigned) < Quantity) - the "N still open" half of the team tab's
	// status summary (RosterBuilderBody), computed once in
	// buildRosterBuilderData rather than re-counting in the template.
	OpenPositionCount int

	// ImportableAssignmentCount is how many people Planning Center already
	// has assigned to this plan's positions that aren't reflected here yet -
	// see SetBuilderData.ImportableSongCount, which this mirrors, and
	// buildRosterBuilderData for why it can't just be read off Positions.
	ImportableAssignmentCount int

	// PersonRelationshipTypes/SchedulePreferenceOptions back the "Quick add
	// a relationship" and per-position "+ Add eligible person" panels
	// (RosterBuilderBody) - letting someone relate two people or set a
	// position preference right from the build page instead of a dedicated
	// setup page. Neither is otherwise needed by this page (suggestions/
	// conflicts read relationship data directly, and eligibility candidates
	// already carry their own SchedulePreference string), so both are
	// fetched only for these panels.
	PersonRelationshipTypes   []scheduling.PersonRelationshipType
	SchedulePreferenceOptions []SchedulePreferenceOption

	// PersonPairPrompt, when set, offers a one-shot inline "remember that
	// these two work together?" relationship capture right after adding
	// someone to the roster - the people equivalent of
	// SetBuilderData.SongPairPrompt. Rosters have no song-order-style
	// "chained after" concept, so "previous" here means whoever was most
	// recently added to this roster (by creation order) before this add -
	// see AddRosterAssignment (rosters.go), the only handler that ever
	// sets this.
	PersonPairPrompt *PersonPairPromptView
}

// PersonPairPromptView is RosterBuilderData.PersonPairPrompt's payload -
// mirrors SongPairPromptView (set_types.go) for people.
type PersonPairPromptView struct {
	PreviousPersonID   string
	PreviousPersonName string
	NewPersonID        string
	NewPersonName      string
}

// PositionGroupView is one open slot - a team + position name, how many are
// needed, who's currently filling it, and (for positions with room left)
// the pool of people eligible to fill it, each annotated with any
// frequency/blockout warning.
type PositionGroupView struct {
	TeamID           string
	TeamName         string
	TeamPositionName string
	// TeamPositionID is the PCO team_position id, resolved by
	// buildRosterBuilderData's name->id lookup (Assigned/Candidates only
	// carry the position's name, not its id) - needed to post a "+ Add
	// eligible person" quick-add straight at this position without asking
	// the user to pick it again. Empty when that lookup failed for this
	// position (best-effort, same as the rest of this page); the quick-add
	// trigger is omitted in that case rather than posting to a blank id.
	TeamPositionID string
	Quantity       int
	Assigned       []AssignmentView
	Candidates     []CandidatePersonView
}

// AssignmentView is one person currently filling a position.
type AssignmentView struct {
	ID       int64
	PersonID string
	Name     string
	Status   string
	// TeamName/TeamPositionName repeat the values already on this
	// assignment's own PositionGroupView - carried here too so the person
	// detail sidebar card (person_detail_card.templ) can say which position
	// it's showing without needing the group it came from.
	TeamName         string
	TeamPositionName string
	// Published is true once this assignment has been pushed to Planning
	// Center as a real PlanPerson - see SetSongView.Published.
	Published bool
	// Notes is this assignment's Planning Center PlanPerson notes -
	// read-only display only (see buildRosterBuilderData), "" whenever this
	// assignment isn't published yet (Planning Center has nothing to carry
	// notes on until then) or has none set. Shown on the person detail
	// sidebar card - see SetSongView.Notes, which this mirrors for people.
	Notes string
	// FrequencyWarning/BlockoutWarning/CrossTeamWarning are empty when
	// there's nothing to flag - see the controller's
	// frequencyWarning/blockoutWarning/crossTeamWarning. CrossTeamWarning
	// looks across every team/position (not just this one), flagging when
	// this person is also serving elsewhere the same calendar week - see
	// ideas.md's "Cross-team burnout".
	FrequencyWarning string
	BlockoutWarning  string
	CrossTeamWarning string
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
	CrossTeamWarning   string
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
