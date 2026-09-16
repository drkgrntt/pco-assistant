// Package notify sends outbound notification emails via Mailgun - just
// one kind so far (feedback submissions, see SendFeedback). Modeled on
// how internal/billing wraps its own outbound API: a Client struct
// constructed once from env vars in server.New and handed to a
// controller in routes.go, rather than the ad hoc os.Getenv-in-the-
// handler shape a sibling project uses for the same kind of email
// sending. Named notify rather than emails since this codebase has no
// existing "emails" package to be confused with.
package notify

import (
	"context"
	"fmt"
	"html"
	"time"

	"pco-assistant/internal/logger"

	"github.com/mailgun/mailgun-go/v4"
)

// sendTimeout bounds a single Mailgun call. SendFeedback is normally
// dispatched from a goroutine (see the feedback controller) rather than
// tied to the HTTP request's own context, so it needs a bound of its
// own rather than inheriting one.
const sendTimeout = 10 * time.Second

// Client is this app's one Mailgun-facing entry point.
type Client struct {
	mg             mailgun.Mailgun
	mgDomain       string
	fromDomain     string
	recipientEmail string
}

// New constructs a Client. mgDomain/mgKey are Mailgun API credentials
// (see README's Setup section); recipientEmail is the address feedback
// submissions are sent to. This app is self-hosted (see README), so
// unlike a hosted product there's no single "the developer's own inbox"
// to fall back to - leaving mgDomain or mgKey blank (e.g. local dev with
// no Mailgun account configured) just means hasCredentials reports
// false and SendFeedback logs the submission instead of calling Mailgun,
// so local dev never needs real credentials and never risks emailing
// anyone by accident.
//
// fromDomain is the domain shown in outbound "From" addresses - separate
// from mgDomain because Mailgun's sending domain is deliberately a
// subdomain (e.g. mg.resonanceonline.org, so it can carry its own MX/SPF
// records without touching the root domain's real mail setup - see
// docs/pco-api-risks.md-style reasoning in the Mailgun setup docs), while
// mail should still visibly come from the root domain
// (resonanceonline.org). DMARC's default "relaxed" alignment treats a
// subdomain and its root as the same organizational domain, so this
// doesn't need any DNS change beyond what mgDomain already has - it's
// purely a display change. Falls back to mgDomain when blank, so
// existing setups (and tests) that only configure mgDomain keep working
// unchanged.
func New(mgDomain, mgKey, fromDomain, recipientEmail string) *Client {
	var mg mailgun.Mailgun
	if mgDomain != "" && mgKey != "" {
		mg = mailgun.NewMailgun(mgDomain, mgKey)
	}
	if fromDomain == "" {
		fromDomain = mgDomain
	}
	return &Client{mg: mg, mgDomain: mgDomain, fromDomain: fromDomain, recipientEmail: recipientEmail}
}

// hasCredentials reports whether a Mailgun domain/key were configured -
// gated on this (not on APP_ENV), so the decision is "can we actually
// send" rather than an environment guess.
func (c *Client) hasCredentials() bool {
	return c.mg != nil
}

// SendFeedback emails a feedback submission to Client's configured
// recipient. submitterEmail is optional (the submitter didn't ask for a
// reply) and shows as "no reply requested" in the email body when blank.
// With no Mailgun credentials configured, this logs the submission
// instead of sending anything - see New's doc comment.
func (c *Client) SendFeedback(ctx context.Context, submitterEmail, message string) error {
	if !c.hasCredentials() {
		logger.InfoContext(ctx, "feedback received (Mailgun not configured - logging instead of emailing)", "from", submitterEmail, "message", message)
		return nil
	}

	sender := fmt.Sprintf("Resonance Feedback <feedback@%s>", c.fromDomain)
	m := mailgun.NewMessage(sender, "Resonance feedback", "", c.recipientEmail)
	m.SetHTML(feedbackHTML(submitterEmail, message))

	sendCtx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()

	_, _, err := c.mg.Send(sendCtx, m)
	return err
}

// feedbackHTML is the email body - kept as a small string builder here
// rather than its own templ component/package, since this is the only
// outbound email in the app and a whole templating convention would be
// overkill for one. submitterEmail/message are both free-form user
// input, so both are HTML-escaped before being embedded.
func feedbackHTML(submitterEmail, message string) string {
	from := submitterEmail
	if from == "" {
		from = "no reply requested"
	}

	return fmt.Sprintf(
		`<div style="font-family: sans-serif;">
	<h2>Feedback from Resonance</h2>
	<p>From: %s</p>
	<p style="white-space: pre-wrap;">%s</p>
</div>`,
		html.EscapeString(from),
		html.EscapeString(message),
	)
}
