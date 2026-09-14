// Package feedback persists /feedback submissions to the feedback table -
// a durable archive alongside (not instead of) the existing Mailgun email
// notification (see internal/notify), so a submission can be revisited
// later without digging through email. Requested in docs/issues.md:
// "add a feedback db table to the app so I can revisit feedback at some
// point without having to dig through my email."
//
// Unlike every other table in this app, feedback isn't one organization's
// own data to show that org's own signed-in admins (see CLAUDE.md's
// Organization scoping section) - it's the app operator's own record
// spanning every org, and the main multi-tenant app has no "app operator,
// not just an org admin" access-control concept to safely gate a view
// with (organization_id/organization_name/person_id/person_name are
// recorded purely for that kind of ad-hoc context on read, not as a query
// scope). List exists for exactly one caller: cmd/admin, a separate
// shared-secret-gated binary outside the main app's auth chain entirely -
// see docs/plans/in-progress/admin-tooling-plan.md for why. The main app
// (internal/server/controllers/feedback.go) only ever calls Create.
package feedback

import (
	"context"
	"database/sql"
	"time"
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

// Feedback is one row of the feedback table, as read back by List.
type Feedback struct {
	ID               int64
	OrganizationID   string
	OrganizationName string
	PersonID         string
	PersonName       string
	SubmitterEmail   string
	Message          string
	CreatedAt        time.Time
}

// List returns every feedback submission, newest first. See the package
// doc comment for why this exists at all (cmd/admin, not the main app) and
// why it's unscoped - feedback spans every organization by design.
func (s *Store) List(ctx context.Context) ([]Feedback, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, organization_id, organization_name, person_id, person_name, submitter_email, message, created_at
		FROM feedback
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Feedback
	for rows.Next() {
		var f Feedback
		if err := rows.Scan(&f.ID, &f.OrganizationID, &f.OrganizationName, &f.PersonID, &f.PersonName, &f.SubmitterEmail, &f.Message, &f.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
