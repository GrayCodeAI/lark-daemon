package service

import (
	"context"
	"fmt"
	"os"

	"github.com/stripe/stripe-go/v78"
	billingportalsession "github.com/stripe/stripe-go/v78/billingportal/session"
	checkoutsession "github.com/stripe/stripe-go/v78/checkout/session"
	"github.com/stripe/stripe-go/v78/webhook"

	"lark-daemon/internal/proto"
)

// StripeService handles Stripe payment operations.
type StripeService struct {
	secretKey       string
	webhookSecret   string
	frontendURL     string
	proPriceID      string
	enterprisePriceID string
}

// NewStripeService creates a StripeService from environment variables.
// LARK_STRIPE_SECRET_KEY: Stripe secret key
// LARK_STRIPE_WEBHOOK_SECRET: Webhook signing secret
// LARK_FRONTEND_URL: Frontend URL for success/cancel redirects
// LARK_STRIPE_PRO_PRICE_ID: Stripe Price ID for Pro plan
// LARK_STRIPE_ENTERPRISE_PRICE_ID: Stripe Price ID for Enterprise plan
func NewStripeService() *StripeService {
	svc := &StripeService{
		secretKey:         os.Getenv("LARK_STRIPE_SECRET_KEY"),
		webhookSecret:     os.Getenv("LARK_STRIPE_WEBHOOK_SECRET"),
		frontendURL:       os.Getenv("LARK_FRONTEND_URL"),
		proPriceID:        os.Getenv("LARK_STRIPE_PRO_PRICE_ID"),
		enterprisePriceID: os.Getenv("LARK_STRIPE_ENTERPRISE_PRICE_ID"),
	}
	if svc.secretKey != "" {
		stripe.Key = svc.secretKey
	}
	if svc.frontendURL == "" {
		svc.frontendURL = "http://localhost:5173"
	}
	return svc
}

// IsConfigured returns true if Stripe is configured.
func (s *StripeService) IsConfigured() bool {
	return s.secretKey != ""
}

// IsWebhookConfigured returns true if webhook verification is configured.
func (s *StripeService) IsWebhookConfigured() bool {
	return s.webhookSecret != ""
}

// CreateCheckoutSession creates a Stripe Checkout Session for the given plan.
func (s *StripeService) CreateCheckoutSession(ctx context.Context, workspaceID, plan string) (string, error) {
	priceID := s.priceIDForPlan(plan)
	if priceID == "" {
		return "", fmt.Errorf("no price configured for plan: %s", plan)
	}

	params := &stripe.CheckoutSessionParams{
		ClientReferenceID: stripe.String(workspaceID),
		Mode:              stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		SuccessURL:        stripe.String(s.frontendURL + "/ws/" + workspaceID + "/billing?success=true"),
		CancelURL:         stripe.String(s.frontendURL + "/ws/" + workspaceID + "/billing?canceled=true"),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				Price:    stripe.String(priceID),
				Quantity: stripe.Int64(1),
			},
		},
	}
	params.Context = ctx

	session, err := checkoutsession.New(params)
	if err != nil {
		return "", fmt.Errorf("create checkout session: %w", err)
	}
	return session.URL, nil
}

// CreatePortalSession creates a Stripe Billing Portal session.
func (s *StripeService) CreatePortalSession(ctx context.Context, stripeCustomerID string) (string, error) {
	params := &stripe.BillingPortalSessionParams{
		Customer:  stripe.String(stripeCustomerID),
		ReturnURL: stripe.String(s.frontendURL + "/billing"),
	}
	params.Context = ctx

	session, err := billingportalsession.New(params)
	if err != nil {
		return "", fmt.Errorf("create portal session: %w", err)
	}
	return session.URL, nil
}

// VerifyWebhookSignature verifies a Stripe webhook signature and returns the event.
func (s *StripeService) VerifyWebhookSignature(payload []byte, signatureHeader string) (stripe.Event, error) {
	return webhook.ConstructEvent(payload, signatureHeader, s.webhookSecret)
}

// priceIDForPlan returns the Stripe Price ID for a given plan.
func (s *StripeService) priceIDForPlan(plan string) string {
	switch plan {
	case "pro":
		return s.proPriceID
	case "enterprise":
		return s.enterprisePriceID
	default:
		return ""
	}
}

// GetPlanForPriceID returns the plan name for a Stripe Price ID.
func (s *StripeService) GetPlanForPriceID(priceID string) proto.BillingPlan {
	if priceID == s.enterprisePriceID {
		return proto.PlanEnterprise
	}
	if priceID == s.proPriceID {
		return proto.PlanPro
	}
	return proto.PlanFree
}
