package controllers

import (
	"context"
	"log"
	"strings"

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

// SubmitFeedback sends the actual email in a goroutine (see
// notify.Client.SendFeedback) rather than waiting on it inline, so the
// htmx response - and the "Thanks - sent!" the submitter sees - doesn't
// block on Mailgun's own network round trip. There's no job queue in
// this codebase to hand this off to instead (see internal/server/jobs,
// which is periodic gocron scheduling only), and a plain goroutine is
// simple enough not to need one just for this.
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

	go func() {
		if err := fc.client.SendFeedback(context.Background(), email, message); err != nil {
			log.Println("error sending feedback email:", err.Error())
		}
	}()

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
