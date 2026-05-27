package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"lark-daemon/internal/proto"
)

// --- Billing ---

// handleGetBilling returns the billing status for a workspace.
func (r *Router) handleGetBilling(w http.ResponseWriter, req *http.Request) {
	workspaceID := chi.URLParam(req, "id")
	customer, err := r.services.GetBillingCustomer(req.Context(), workspaceID)
	if err != nil {
		serverError(w, err, "get billing")
		return
	}
	if customer == nil {
		// Return default free plan
		customer = &proto.BillingCustomer{
			WorkspaceID: workspaceID,
			Plan:        proto.PlanFree,
			Status:      proto.BillingActive,
		}
	}
	// Include plan limits
	limits := proto.GetPlanLimits(customer.Plan)
	writeJSON(w, http.StatusOK, map[string]any{
		"customer": customer,
		"limits":   limits,
	})
}

// handleCreateCheckout creates a Stripe checkout session.
func (r *Router) handleCreateCheckout(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	if member.Role != proto.RoleAdmin && member.Role != proto.RoleOwner {
		writeError(w, http.StatusForbidden, "admin required")
		return
	}
	workspaceID := chi.URLParam(req, "id")
	var body struct {
		Plan string `json:"plan"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Plan != "pro" && body.Plan != "enterprise" {
		writeError(w, http.StatusBadRequest, "plan must be pro or enterprise")
		return
	}
	if !r.stripe.IsConfigured() {
		writeError(w, http.StatusServiceUnavailable, "billing not configured")
		return
	}
	// Create or get billing customer
	customer, err := r.services.GetBillingCustomer(req.Context(), workspaceID)
	if err != nil {
		serverError(w, err, "get billing customer")
		return
	}
	if customer == nil {
		customer = &proto.BillingCustomer{
			WorkspaceID: workspaceID,
			Plan:        proto.PlanFree,
			Status:      proto.BillingActive,
		}
		if err := r.services.CreateBillingCustomer(req.Context(), customer); err != nil {
			serverError(w, err, "create billing customer")
			return
		}
	}
	checkoutURL, err := r.stripe.CreateCheckoutSession(req.Context(), workspaceID, body.Plan)
	if err != nil {
		serverError(w, err, "create checkout session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"checkout_url": checkoutURL,
		"plan":         body.Plan,
		"customer_id":  customer.ID,
	})
}

// handleBillingPortal creates a Stripe billing portal session.
func (r *Router) handleBillingPortal(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	workspaceID := chi.URLParam(req, "id")
	customer, err := r.services.GetBillingCustomer(req.Context(), workspaceID)
	if err != nil {
		serverError(w, err, "get billing")
		return
	}
	if customer == nil || customer.StripeCustomerID == "" {
		writeError(w, http.StatusNotFound, "no billing account")
		return
	}
	if !r.stripe.IsConfigured() {
		writeError(w, http.StatusServiceUnavailable, "billing not configured")
		return
	}
	portalURL, err := r.stripe.CreatePortalSession(req.Context(), customer.StripeCustomerID)
	if err != nil {
		serverError(w, err, "create portal session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"portal_url": portalURL,
	})
}

// handleGetUsage returns usage metrics for a workspace.
func (r *Router) handleGetUsage(w http.ResponseWriter, req *http.Request) {
	workspaceID := chi.URLParam(req, "id")
	records, err := r.services.ListUsageRecords(req.Context(), workspaceID)
	if err != nil {
		serverError(w, err, "get usage")
		return
	}
	customer, _ := r.services.GetBillingCustomer(req.Context(), workspaceID)
	plan := proto.PlanFree
	if customer != nil {
		plan = customer.Plan
	}
	limits := proto.GetPlanLimits(plan)
	writeJSON(w, http.StatusOK, map[string]any{
		"usage":   records,
		"plan":    plan,
		"limits":  limits,
	})
}

// handleStripeWebhook processes Stripe webhook events.
func (r *Router) handleStripeWebhook(w http.ResponseWriter, req *http.Request) {
	if !r.stripe.IsWebhookConfigured() {
		writeError(w, http.StatusServiceUnavailable, "webhook not configured")
		return
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read body")
		return
	}
	// Verify webhook signature
	stripeEvent, err := r.stripe.VerifyWebhookSignature(body, req.Header.Get("Stripe-Signature"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid signature")
		return
	}
	event := struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}{
		Type: string(stripeEvent.Type),
		Data: stripeEvent.Data.Raw,
	}
	switch event.Type {
	case "checkout.session.completed":
		var session struct {
			Customer       string `json:"customer"`
			Subscription   string `json:"subscription"`
			ClientRefID    string `json:"client_reference_id"`
			AmountTotal    int64  `json:"amount_total"`
		}
		if err := json.Unmarshal(event.Data, &session); err == nil && session.ClientRefID != "" {
			customer, _ := r.services.GetBillingCustomer(req.Context(), session.ClientRefID)
			if customer != nil {
				customer.StripeCustomerID = session.Customer
				customer.StripeSubscriptionID = session.Subscription
				customer.Status = proto.BillingActive
				now := time.Now().UnixMilli()
				customer.CurrentPeriodStart = now
				customer.CurrentPeriodEnd = now + 30*24*60*60*1000 // 30 days
				// Determine plan from amount
				if session.AmountTotal >= 10000 { // $100+
					customer.Plan = proto.PlanEnterprise
				} else {
					customer.Plan = proto.PlanPro
				}
				_ = r.services.UpdateBillingCustomer(req.Context(), customer)
			}
		}
	case "customer.subscription.updated":
		var sub struct {
			Customer string `json:"customer"`
			Status   string `json:"status"`
		}
		if err := json.Unmarshal(event.Data, &sub); err == nil {
			customer, _ := r.services.GetBillingCustomerByStripeID(req.Context(), sub.Customer)
			if customer != nil {
				switch sub.Status {
				case "active":
					customer.Status = proto.BillingActive
				case "past_due":
					customer.Status = proto.BillingPastDue
				case "canceled", "unpaid":
					customer.Status = proto.BillingCanceled
				case "trialing":
					customer.Status = proto.BillingTrialing
				}
				_ = r.services.UpdateBillingCustomer(req.Context(), customer)
			}
		}
	case "customer.subscription.deleted":
		var sub struct {
			Customer string `json:"customer"`
		}
		if err := json.Unmarshal(event.Data, &sub); err == nil {
			customer, _ := r.services.GetBillingCustomerByStripeID(req.Context(), sub.Customer)
			if customer != nil {
				customer.Status = proto.BillingCanceled
				customer.Plan = proto.PlanFree
				customer.StripeSubscriptionID = ""
				_ = r.services.UpdateBillingCustomer(req.Context(), customer)
			}
		}
	}
	w.WriteHeader(http.StatusOK)
}

// --- Plan limits middleware ---

// checkPlanLimits returns an error if the workspace has exceeded its plan limits.
func (r *Router) checkPlanLimit(workspaceID, metric string) error {
	customer, _ := r.services.GetBillingCustomer(context.Background(), workspaceID)
	plan := proto.PlanFree
	if customer != nil && customer.Status == proto.BillingActive {
		plan = customer.Plan
	}
	limits := proto.GetPlanLimits(plan)
	var current int
	switch metric {
	case "members":
		members, _ := r.services.ListMembers(context.Background(), workspaceID)
		current = len(members)
		if current >= limits.MaxMembers {
			return fmt.Errorf("plan limit exceeded: members (limit: %d, plan: %s)", limits.MaxMembers, plan)
		}
	case "channels":
		channels, _ := r.services.ListChannels(context.Background(), workspaceID)
		current = len(channels)
		if current >= limits.MaxChannels {
			return fmt.Errorf("plan limit exceeded: channels (limit: %d, plan: %s)", limits.MaxChannels, plan)
		}
	case "messages":
		periodStart := time.Now().AddDate(0, 0, -time.Now().Day()+1).Truncate(24 * time.Hour).UnixMilli()
		usage, _ := r.services.GetUsageRecord(context.Background(), workspaceID, "messages", periodStart)
		if usage != nil && usage.Quantity >= limits.MaxMessages {
			return fmt.Errorf("plan limit exceeded: messages per month (limit: %d, plan: %s)", limits.MaxMessages, plan)
		}
	case "workflows":
		workflows, _ := r.services.ListWorkflows(context.Background(), workspaceID)
		current = len(workflows)
		if current >= limits.MaxWorkflows {
			return fmt.Errorf("plan limit exceeded: workflows (limit: %d, plan: %s)", limits.MaxWorkflows, plan)
		}
	}
	return nil
}

// recordUsage increments usage for a workspace metric. Runs in background, errors are logged but not returned.
func (r *Router) recordUsage(workspaceID, metric string, quantity int64) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		now := time.Now()
		periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).UnixMilli()
		periodEnd := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, now.Location()).UnixMilli()
		_ = r.services.IncrementUsage(ctx, workspaceID, metric, periodStart, periodEnd, int(quantity))
	}()
}
