package controllers

import (
	"strings"

	"pco-assistant/internal/logger"
	"pco-assistant/internal/notify"
	"pco-assistant/internal/server/middleware"
	"pco-assistant/internal/views"

	"github.com/gofiber/fiber/v2"
)

// FeedbackController is a top-level (not /scheduling/...) authenticated
// page - feedback submitters here are signed-in org users, not anonymous
// site visitors, unlike the fully-public feedback form this was modeled
// on in a sibling project.
type FeedbackController struct {
	client *notify.Client
}

func NewFeedbackController(client *notify.Client) *FeedbackController {
	return &FeedbackController{client: client}
}

func (fc *FeedbackController) RegisterRoutes(router fiber.Router) {
	router.Get("/feedback", fc.FeedbackPage)
	router.Post("/feedback", fc.SubmitFeedback)
}

func (fc *FeedbackController) FeedbackPage(c *fiber.Ctx) error {
	_, personName := middleware.CurrentPerson(c)
	return render(c, views.Feedback(personName, middleware.CurrentOrganization(c), ""))
}

type feedbackForm struct {
	Email   string `form:"email"`
	Message string `form:"message"`
}

// SubmitFeedback sends the actual email inline, bounded by
// notify.sendTimeout (10s) - it used to fire this from a goroutine instead
// so the htmx response never waited on Mailgun's own network round trip,
// but that meant a real send failure (see docs/pco-api-risks.md-style
// incidents: a stale/rejected Mailgun key silently failed in production
// for a while, discovered only by checking server logs, not by anyone
// looking at the page) was invisible to the person submitting - they saw
// "Thanks - sent!" either way. Feedback submission isn't a hot path (rare,
// user-initiated, one org admin at a time), so paying a bounded wait here
// to actually tell the submitter when it failed is a better trade than
// staying non-blocking and silently swallowing the failure.
func (fc *FeedbackController) SubmitFeedback(c *fiber.Ctx) error {
	var body feedbackForm
	if err := c.BodyParser(&body); err != nil {
		return render(c, views.FeedbackResult(err.Error(), false))
	}

	if errMsg := feedbackValidationError(body.Message); errMsg != "" {
		return render(c, views.FeedbackResult(errMsg, false))
	}

	email := strings.TrimSpace(body.Email)
	message := body.Message

	ctx := middleware.RequestContext(c)
	if err := fc.client.SendFeedback(ctx, email, message); err != nil {
		logger.ErrorContext(ctx, "sending feedback email failed", "error", err)
		return render(c, views.FeedbackResult("Sending failed - try again in a moment, or reach out directly if it keeps happening.", false))
	}

	return render(c, views.FeedbackResult("", true))
}

// feedbackValidationError reports why a feedback submission can't be
// sent - message is the only required field, email is optional (see
// feedback.templ). Kept separate from SubmitFeedback so it's testable
// without a fiber.Ctx - see feedback_test.go.
func feedbackValidationError(message string) string {
	if strings.TrimSpace(message) == "" {
		return "Message can't be blank."
	}
	return ""
}
