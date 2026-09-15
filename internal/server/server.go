package server

import (
	"os"

	"github.com/gofiber/fiber/v2"

	"pco-assistant/internal/auth"
	"pco-assistant/internal/billing"
	"pco-assistant/internal/database"
	"pco-assistant/internal/feedback"
	"pco-assistant/internal/notify"
	"pco-assistant/internal/scheduling"
)

type FiberServer struct {
	*fiber.App

	db              database.Service
	schedulingStore *scheduling.Store
	authStore       *auth.Store
	billingClient   *billing.Client
	notifyClient    *notify.Client
	feedbackStore   *feedback.Store
}

func New() *FiberServer {
	db := database.New()
	registerPCOResponseHook()

	server := &FiberServer{
		App: fiber.New(fiber.Config{
			ServerHeader: "pco-assistant",
			AppName:      "pco-assistant",
		}),

		db:              db,
		schedulingStore: scheduling.NewStore(db.DB()),
		authStore:       auth.NewStore(db.DB(), auth.LoadTokenEncryptionKeyFromEnv()),
		billingClient: billing.New(
			os.Getenv("STRIPE_SECRET_KEY"),
			os.Getenv("STRIPE_PRICE_STANDARD"),
			os.Getenv("STRIPE_PRICE_MULTI_CAMPUS"),
			os.Getenv("STRIPE_WEBHOOK_SECRET"),
			os.Getenv("APP_BASE_URL"),
		),
		notifyClient: notify.New(
			os.Getenv("MG_DOMAIN"),
			os.Getenv("MG_KEY"),
			os.Getenv("FEEDBACK_RECIPIENT_EMAIL"),
		),
		feedbackStore: feedback.NewStore(db.DB()),
	}

	return server
}
