// Package feedback persists /feedback submissions to the feedback table -
// a durable archive alongside (not instead of) the existing Mailgun email
// notification (see internal/notify), so a submission can be revisited
// later without digging through email. Requested in docs/issues.md:
// "add a feedback db table to the app so I can revisit feedback at some
// point without having to dig through my email."
//
// Deliberately minimal and write-only for now: just Create. There's no
// read method or web-facing view yet - unlike every other table in this
// app, feedback isn't one organization's own data to show that org's own
// signed-in admins (see CLAUDE.md's Organization scoping section); it's
// the app operator's own record spanning every org, and this app has no
// existing "app operator, not just an org admin" access-control concept
// to safely gate a view with. Query the table directly for now
// (organization_id/organization_name/person_id/person_name are recorded
// purely for that kind of ad-hoc context on read, not as a query scope);
// a real in-app view is a separate future decision once how to gate it is
// settled, not something this package tries to anticipate.
package feedback

import (
	"context"
	"database/sql"
)

// Store provides access to feedback submissions. Safe for concurrent use,
// same as the *sql.DB it wraps - mirrors scheduling.Store's shape.
type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// CreateParams is one feedback submission's fields to persist - the same
// data the controller already has in hand from the request/session at
// submit time (see FeedbackController.SubmitFeedback), not re-derived
// here.
type CreateParams struct {
	OrganizationID   string
	OrganizationName string
	PersonID         string
	PersonName       string
	// SubmitterEmail is the optional reply-to address from the form -
	// "" when the submitter didn't ask for a reply, same as
	// notify.Client.SendFeedback's own submitterEmail param.
	SubmitterEmail string
	Message        string
}

// Create inserts one feedback row. Called best-effort alongside the
// actual email send (see SubmitFeedback) - a failure here shouldn't block
// or fail the submitter's response, since the email is still the primary
// real-time notification; the caller is responsible for logging a failure
// so it doesn't rot silently.
func (s *Store) Create(ctx context.Context, params CreateParams) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO feedback (organization_id, organization_name, person_id, person_name, submitter_email, message)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, params.OrganizationID, params.OrganizationName, params.PersonID, params.PersonName, params.SubmitterEmail, params.Message)
	return err
}
