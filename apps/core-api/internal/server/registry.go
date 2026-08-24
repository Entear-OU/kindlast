// Package server wires the Connect handlers that core-api serves.
//
// At this point it holds only the service registry: the list of proto
// services this binary exposes. The handlers themselves, and the interceptor
// chain that fronts them, arrive with ENT-195 and ENT-196.
package server

import (
	"google.golang.org/protobuf/reflect/protoreflect"

	corev1 "github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1"
	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
)

// Services returns every proto service core-api exposes.
//
// It exists so the scope-declaration test has one place to enumerate, rather
// than a list in a test file that someone forgets to extend. Adding a service
// here without declaring scopes on its methods fails that test, which is the
// whole point: the check must be impossible to skip by forgetting.
func Services() []protoreflect.ServiceDescriptor {
	files := []protoreflect.FileDescriptor{
		corev1.File_kindlast_core_v1_session_proto,
		corev1.File_kindlast_core_v1_org_proto,
		// Listed from the moment the contract exists rather than when its
		// handlers do. The scope-declaration test then covers FindingsService
		// and DashboardService immediately, so an RPC cannot reach main
		// undeclared during the window where the proto has landed and the
		// service has not.
		corev1.File_kindlast_core_v1_findings_proto,
		// RecordsService, listed under the same rule while it is contract only
		// (ENT-200). Its handlers are not registered on the mux yet, so nothing
		// here is reachable; what this buys is that the six RPCs are scope
		// checked now, and the day a handler is added it cannot arrive
		// undeclared.
		corev1.File_kindlast_core_v1_records_proto,
		// ConversationService (ENT-270). Asking the Analyst about one finding,
		// on `agents:ask`. A separate scope from `findings:read` because
		// reading a finding and running a model over it are separately
		// dangerous, which is the same reasoning that keeps `findings:read` and
		// `findings:act` apart.
		corev1.File_kindlast_core_v1_conversation_proto,
		// ApprovalService (ENT-278). Asking the Hands what approving one
		// finding will do, on `agents:ask` for the same reason asking the
		// Analyst needs it: running a model over a finding is separately
		// dangerous from reading one. Listed here so the scope-declaration
		// test covers it, which is the check ENT-245 was filed for after
		// NarrativeService shipped mounted and unlisted and every call to it
		// was default-denied.
		corev1.File_kindlast_core_v1_approvals_proto,
		// NotificationService (ENT-209). Its three RPCs carry
		// `notifications:read` and `notifications:write`, which were already in
		// HumanScopes and in the Zitadel seed before anything used them.
		corev1.File_kindlast_core_v1_notifications_proto,
		// BillingService (ENT-210). Read only: a plan changes because the signed
		// webhook said so, never because a session asked.
		corev1.File_kindlast_core_v1_billing_proto,
		// AuditService (ENT-223). Read only, and structurally so: the table is
		// append-only by trigger and `kindlast_app` holds no update or delete
		// grant on it. Both RPCs carry `audit:read`, which was seeded before
		// anything used it.
		corev1.File_kindlast_core_v1_audit_proto,
		// CorpusService (ENT-207). Read only, on `corpus:read`. The write side
		// is IngestService on the platform surface, which is the separation the
		// product's central claim rests on: a console request must not be able
		// to change the law a finding is checked against.
		corev1.File_kindlast_core_v1_corpus_proto,
		// MemoryService (ENT-228). What Kindlast knows about the organisation:
		// three reads on `memory:read` and one typed patch on `memory:write`.
		// The patch is a human scope on purpose, because correcting a fact
		// about yourself is rectification rather than a privileged operation.
		corev1.File_kindlast_core_v1_memory_proto,
		// OnboardingService (ENT-212). The first conversation, and the surface
		// that feeds the profile MemoryService reads. Three writes on
		// `onboarding:write` and one read on `onboarding:read`; the read is
		// separate because every authenticated route asks it before deciding
		// whether to route a person into onboarding, and a route that had to
		// hold a write scope to find out where somebody had got to would be a
		// scope granted for a question rather than for an action.
		corev1.File_kindlast_core_v1_onboarding_proto,
		// IntegrationsService (ENT-231). Which of a customer's systems Kindlast
		// may reach, and what it may do there. On the core surface because it
		// is the customer's own control panel, and every method is bounded by
		// membership and RLS like the rest of it. The service that actually
		// dials out is GatewayService, which this binary does not serve.
		corev1.File_kindlast_core_v1_integrations_proto,
		// ModelService (ENT-236). Where an organisation's model runs, on
		// `model:read` and `model:write`. Both are human scopes; what makes the
		// write owner-only is a role check in Go, because a scope bounds what a
		// client may do and this bounds which person may.
		corev1.File_kindlast_core_v1_model_proto,
		// ApiKeyService (ENT-262). The third token model: a partner's key,
		// listed on `org:read` and minted and revoked on `org:manage`.
		//
		// `org:manage` is deliberately not a scope a key may itself carry, so a
		// key can see the list and can never add to it. That is enforced in Go
		// (apikey.GrantableScopes), by a CHECK constraint in 00043, and again
		// in the handler, which is three refusals for the one property that
		// stops a credential multiplying itself.
		corev1.File_kindlast_core_v1_apikeys_proto,
		// The internal surface is enumerated here too, so the scope-declaration
		// test covers it. An internal RPC is the last place an undeclared scope
		// should be able to hide: these carry `internal:*`, which is the
		// vocabulary that can act across organisations.
		platformv1.File_kindlast_platform_v1_sweep_proto,
		// DeliveryService (ENT-256, part three). The outbox's delivery half,
		// called by the Temporal worker on `internal:ingest`: list what is
		// pending, deliver one, reclaim what no longer needs keeping.
		platformv1.File_kindlast_platform_v1_delivery_proto,
		// IngestService (ENT-207). Writing the corpus, on `internal:ingest`.
		// Listed here for the same reason as the sweep: the internal surface is
		// the last place an undeclared scope should be able to hide, because it
		// is the vocabulary that acts outside any one organisation.
		platformv1.File_kindlast_platform_v1_ingest_proto,
		// NarrativeService (ENT-245). Drafting the prose a finding carries, on
		// `internal:intelligence`.
		//
		// It shipped mounted on the mux and missing from this list, and the
		// scope table is built from this list, so the interceptor default-denied
		// every call: permission_denied, "declares no required scope", in every
		// deployment, while the service's own tests stayed green. The deny was
		// right. Being absent here is what was wrong.
		//
		// TestEveryKindlastServiceIsClassified now walks the global proto
		// registry and fails on any Kindlast service that is neither listed here
		// nor explicitly excused, so the next one cannot arrive this way.
		platformv1.File_kindlast_platform_v1_narrative_proto,
		// CompletionService (ENT-256, part five). One model call, on
		// `internal:intelligence`, with the organisation's key opened here in
		// Go and nowhere else.
		platformv1.File_kindlast_platform_v1_completion_proto,
		// ExecutorService (ENT-271). Creating the record an approved finding
		// asked for, on `internal:ingest`, as the approver named on the job.
		platformv1.File_kindlast_platform_v1_executor_proto,
		// WatcherService (ENT-258). What an agentic Watcher reads, and the
		// one thing it may write: a signal, never a finding.
		platformv1.File_kindlast_platform_v1_watcher_proto,
		// HandsService (ENT-261). What approving a finding will do, and the
		// record it prepares. On `internal:ingest`, and deliberately not on
		// anything that approves: the Hands explains and prepares, and the
		// decision stays a human's.
		platformv1.File_kindlast_platform_v1_hands_proto,
	}

	var services []protoreflect.ServiceDescriptor
	for _, file := range files {
		descriptors := file.Services()
		for i := 0; i < descriptors.Len(); i++ {
			services = append(services, descriptors.Get(i))
		}
	}
	return services
}
