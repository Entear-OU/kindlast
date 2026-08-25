package server_test

import (
	"testing"

	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/server"
	"github.com/Entear-OU/kindlast/libs/chassis/httprule"
)

// The REST binding half of ENT-199, built to the same shape as the scope
// declaration test next to it, because the failure it guards is the same
// shape: an RPC added without an annotation, noticed by nobody.
//
// What makes this worth having when the annotations already generate an
// OpenAPI document: generation does not fail on a missing annotation. It
// quietly emits a document describing a smaller API than the one that exists,
// and the gap surfaces as an endpoint absent from a customer's generated
// client, long after the commit that caused it.

func TestEveryRPCDeclaresAnHTTPBinding(t *testing.T) {
	services := server.Services()
	if len(services) == 0 {
		t.Fatal("no services registered; the registry is the thing under test")
	}

	var undeclaredTotal []string
	for _, service := range services {
		bindings, undeclared := httprule.OfService(service)
		undeclaredTotal = append(undeclaredTotal, undeclared...)

		if service.Methods().Len() > 0 && len(bindings) == 0 && len(undeclared) == 0 {
			t.Fatalf("%s has methods but produced neither bindings nor failures", service.FullName())
		}
	}

	if len(undeclaredTotal) > 0 {
		t.Fatalf("RPCs missing a google.api.http annotation: %v", undeclaredTotal)
	}
}

// The other half, and the one the presence check cannot give.
//
// A reader hard-wired to return one binding passes "is something declared"
// forever. So this asserts the actual values the shipped contract carries: if
// the reader stops reading and starts guessing, these go red, and if a path is
// changed by accident the diff has to say so here too.
//
// The paths are the ones §12 and §0.3 settle: `/api/v1/...`, and `:verb` for a
// custom action rather than a subresource that does not exist.
func TestTheDeclaredBindingsAreTheOnesTheContractPromises(t *testing.T) {
	want := map[string]httprule.Binding{
		"kindlast.core.v1.SessionService.GetCurrentUser": {
			Method: "GET", Path: "/api/v1/me",
		},
		"kindlast.core.v1.OrgService.AcceptInvitation": {
			Method: "POST", Path: "/api/v1/invitations/{token}:accept",
		},

		// ENT-202. Read these as a group, because the thing worth reviewing is
		// what they all lack: not one carries an `{org_id}` segment.
		//
		// Conventional REST would write /organisations/{org_id}/members/{user_id}
		// and it would be wrong here. The organisation travels in the
		// Kindlast-Org-Id header so that membership has exactly one source of
		// truth; a path parameter would give the same fact a second source, and
		// a request naming one organisation in the header and another in the
		// path would have to be either an error nobody wrote or a silent
		// winner. Stripe and Slack scope the same way.
		//
		// `{user_id}` is fine and is a different thing: it names the person
		// being acted on, and it is meaningless without the header.
		"kindlast.core.v1.OrgService.CreateOrganisation": {
			Method: "POST", Path: "/api/v1/organisations",
		},
		// Singular, because it addresses the organisation the header names
		// rather than a member of a collection.
		"kindlast.core.v1.OrgService.UpdateOrganisation": {
			Method: "PATCH", Path: "/api/v1/organisation",
		},
		"kindlast.core.v1.OrgService.ListMembers": {
			Method: "GET", Path: "/api/v1/members",
		},
		"kindlast.core.v1.OrgService.UpdateMemberRole": {
			Method: "PATCH", Path: "/api/v1/members/{user_id}",
		},
		"kindlast.core.v1.OrgService.RemoveMember": {
			Method: "DELETE", Path: "/api/v1/members/{user_id}",
		},
		"kindlast.core.v1.OrgService.InviteMember": {
			Method: "POST", Path: "/api/v1/invitations",
		},

		// ENT-262, partner API keys. Two things here are worth reviewing rather
		// than merely generating.
		//
		// No `{org_id}`, like every other binding on this surface, and for keys
		// that absence is load-bearing twice over. On these three RPCs the
		// organisation comes from `Kindlast-Org-Id` as usual. On a request that
		// ARRIVES on a key, the organisation comes from the key itself and a
		// header naming a different one is refused, which is what stops one
		// partner credential reaching every client company a consultancy
		// serves.
		//
		// DELETE on the resource rather than a `:revoke` verb. AIP-136 reserves
		// the custom verb for an action that is not one of the standard
		// methods, and revoking a credential is a delete from the caller's
		// point of view: the thing they hold stops existing. That the row
		// survives with `revoked_at` set is the schema keeping evidence, and
		// not something the contract should make a caller think about.
		"kindlast.core.v1.ApiKeyService.ListApiKeys": {
			Method: "GET", Path: "/api/v1/api-keys",
		},
		"kindlast.core.v1.ApiKeyService.CreateApiKey": {
			Method: "POST", Path: "/api/v1/api-keys",
		},
		"kindlast.core.v1.ApiKeyService.RevokeApiKey": {
			Method: "DELETE", Path: "/api/v1/api-keys/{key_id}",
		},

		// ENT-203. Same absence to review as the group above: no `{org_id}`
		// anywhere. `{finding_id}` names the thing being acted on and is
		// meaningless without the header, exactly as `{user_id}` is.
		//
		// The three act paths use `:approve`, `:reject` and `:snooze` rather
		// than `/approve`, per AIP-136 and §12: a custom verb on a resource,
		// not a subresource that does not exist. `AcceptInvitation` above set
		// that precedent.
		"kindlast.core.v1.FindingsService.ListFindings": {
			Method: "GET", Path: "/api/v1/findings",
		},
		"kindlast.core.v1.FindingsService.GetFinding": {
			Method: "GET", Path: "/api/v1/findings/{finding_id}",
		},
		"kindlast.core.v1.FindingsService.ApproveFinding": {
			Method: "POST", Path: "/api/v1/findings/{finding_id}:approve",
		},
		"kindlast.core.v1.FindingsService.RejectFinding": {
			Method: "POST", Path: "/api/v1/findings/{finding_id}:reject",
		},
		"kindlast.core.v1.FindingsService.SnoozeFinding": {
			Method: "POST", Path: "/api/v1/findings/{finding_id}:snooze",
		},
		// ENT-270. A fourth colon verb on the same resource, and it belongs on
		// the finding rather than on the agent: the subject of the question is
		// this finding, and `/agents/analyst:ask` would be a URL naming who
		// answers instead of what is being asked about. It is a different
		// service because it is a different scope and a different dependency,
		// not because it is a different resource.
		"kindlast.core.v1.ConversationService.AskAboutFinding": {
			Method: "POST", Path: "/api/v1/findings/{finding_id}:ask",
		},
		// ENT-278. A fifth colon verb on the finding, beside the three that
		// decide it, because that is what it is about: what pressing approve
		// will do. On the finding rather than on `/agents/hands:explain` for
		// the reason the line above gives, and deliberately adjacent to
		// `:approve` in this list, since the pair is the thing to review. One
		// runs an agent that cannot decide; the other is the decision, on
		// `findings:act`, which only a human's token carries.
		"kindlast.core.v1.ApprovalService.ExplainApproval": {
			Method: "POST", Path: "/api/v1/findings/{finding_id}:explain-approval",
		},
		// Singular, and for the same reason UpdateOrganisation is: it
		// addresses the dashboard of the organisation the header names, not a
		// member of a collection of dashboards.
		"kindlast.core.v1.DashboardService.GetDashboard": {
			Method: "GET", Path: "/api/v1/dashboard",
		},

		// ENT-200, the records read surface. Pinned while the service is
		// contract only and no handler is registered, which is the window this
		// test exists to cover: the paths are reviewed now rather than
		// inherited unexamined by whoever writes the handlers.
		//
		// The reviewable choice is the `/records/` prefix. Three registers sit
		// under one collection because they are one answer to one question,
		// what is on file for this organisation, and they share `records:read`.
		// Flat paths (`/api/v1/processing-activities`) would read as three
		// unrelated collections and would leave no room for a records-wide
		// endpoint later.
		//
		// Same absence to review as every group above: no `{org_id}` anywhere.
		// Each `{..._id}` names the record being read and is meaningless
		// without the header.
		//
		// Kebab-case in the path (`ai-systems`, `processing-activities`) where
		// the proto field is snake_case, which is the convention the rest of
		// this API already follows for multi-word segments.
		"kindlast.core.v1.RecordsService.ListProcessingActivities": {
			Method: "GET", Path: "/api/v1/records/processing-activities",
		},
		"kindlast.core.v1.RecordsService.GetProcessingActivity": {
			Method: "GET", Path: "/api/v1/records/processing-activities/{processing_activity_id}",
		},
		"kindlast.core.v1.RecordsService.ListAiSystems": {
			Method: "GET", Path: "/api/v1/records/ai-systems",
		},
		"kindlast.core.v1.RecordsService.GetAiSystem": {
			Method: "GET", Path: "/api/v1/records/ai-systems/{ai_system_id}",
		},
		// Notifications (ENT-209). A singleton resource rather than a
		// collection: the preferences being read and written are always the
		// caller's own, for the organisation the header names, so there is
		// nothing to identify in the path. A `/{user_id}` segment would offer a
		// client somebody else's id to try, and make this handler the thing that
		// refuses when the policy already does.
		//
		// PUT rather than PATCH, unlike the records surface above, because here
		// the body genuinely IS the resource: every field is client-owned and
		// the settings page renders all of them at once. There are no
		// server-owned columns to preserve, so the promise PUT makes is one this
		// endpoint can keep.
		"kindlast.core.v1.NotificationService.GetNotificationPreferences": {
			Method: "GET", Path: "/api/v1/notification-preferences",
		},
		"kindlast.core.v1.NotificationService.UpdateNotificationPreferences": {
			Method: "PUT", Path: "/api/v1/notification-preferences",
		},
		// Deployment-wide rather than tenant-scoped: what this installation can
		// deliver on is the same answer for every organisation in it.
		"kindlast.core.v1.NotificationService.GetNotificationCapabilities": {
			Method: "GET", Path: "/api/v1/notification-capabilities",
		},

		// The caller's own linked channels (ENT-263). A collection under the
		// person rather than under a user id, like the preferences above and
		// for a sharper reason: a path segment naming a member would be an
		// endpoint for enumerating which colleagues are reachable on Telegram
		// and at which chat. There is nowhere to put one.
		"kindlast.core.v1.NotificationService.ListLinkedChannels": {
			Method: "GET", Path: "/api/v1/notification-channels",
		},
		// POST and DELETE on the channel itself, because linking creates the
		// caller's one Telegram channel and unlinking removes it. Not PUT:
		// linking mints a verification code as a side effect, which is not
		// something a caller can repeat idempotently and get the same answer.
		"kindlast.core.v1.NotificationService.LinkTelegramChat": {
			Method: "POST", Path: "/api/v1/notification-channels/telegram",
		},
		"kindlast.core.v1.NotificationService.UnlinkTelegramChat": {
			Method: "DELETE", Path: "/api/v1/notification-channels/telegram",
		},
		// A custom method on the same resource, in the `:verb` form, because
		// proving you hold a chat is an action on the channel rather than a
		// write to any field of it. The alternative was a PUT of a `verified`
		// flag, which would describe the caller as setting the thing they are
		// asking the server to decide.
		"kindlast.core.v1.NotificationService.VerifyTelegramChat": {
			Method: "POST", Path: "/api/v1/notification-channels/telegram:verify",
		},

		// Billing (ENT-210). A singleton like the notification preferences
		// above, and read-only: there is no PUT, because a plan changes when
		// the signed webhook says so and never because a session asked.
		//
		// The webhook itself is deliberately absent from this table. It is not
		// an RPC and carries no `google.api.http` annotation: it is a plain
		// route on the mux, registered only when billing is configured, and
		// routed at the edge by exactly one path rather than by a wildcard.
		"kindlast.core.v1.BillingService.GetBilling": {
			Method: "GET", Path: "/api/v1/billing",
		},

		// The audit log (ENT-223). A GET on the collection, and an export that
		// is a POST for two reasons rather than one.
		//
		// The first is the ordinary one: the filter is a structured object and
		// belongs in a body. The second matters more here. An export of an
		// audit log is a request for a file containing a whole organisation's
		// decision history, and a GET is the method that gets logged in proxy
		// access logs, retried by link scanners, and put in a browser's history
		// with its query string intact. A filter naming a specific person and a
		// date range is not a thing to leave in three intermediaries' logs.
		//
		// The colon verb rather than `/api/v1/audit/export`, because this is an
		// action on the collection and not a subresource. There is nothing at
		// `/audit/export` to GET.
		// The regulatory corpus (ENT-207). Plain GETs on a collection and a
		// member, which is the whole shape: this is reference data and there is
		// nothing to act on.
		//
		// Keyed by `slug` rather than by a uuid, and that is the one choice
		// worth pinning. An obligation's slug is stable across rewordings of its
		// text, so `/corpus/obligations/gdpr-art-30-ropa` is a URL somebody can
		// put in a document and expect to still work; the row's id is generated
		// per deployment and would differ between two installations reading the
		// same law.
		//
		// Still no {org_id}, and here for a stronger reason than elsewhere: the
		// corpus has none. It is the same regulation for every customer.
		"kindlast.core.v1.CorpusService.ListObligations": {
			Method: "GET", Path: "/api/v1/corpus/obligations",
		},
		"kindlast.core.v1.CorpusService.GetObligation": {
			Method: "GET", Path: "/api/v1/corpus/obligations/{slug}",
		},
		"kindlast.core.v1.CorpusService.ListDocuments": {
			Method: "GET", Path: "/api/v1/corpus/documents",
		},

		// What Kindlast knows about the organisation (ENT-228).
		//
		// Still no {org_id} in the path, for the reason every other binding
		// here has none: the organisation comes from the header on every
		// request and is checked against membership, so a URL that named one
		// would be a second answer to the same question and the two could
		// disagree.
		//
		// `facts` and `evidence` are separate collections rather than one
		// `/memory` with a filter, because they are different things: what we
		// believe is a small correctable set, what we observed is an unbounded
		// append-only log. A single collection would have to page the first
		// and would invite correcting the second.
		//
		// The history path is keyed by the fact key rather than by a row id,
		// and that is the one choice worth pinning. A key is stable and
		// meaningful, so `/memory/facts/PROFILE_FACT_KEY_HAS_DPO/history` is a
		// URL somebody can hold onto; the id of the currently open row changes
		// every time the fact is corrected, which is precisely when somebody
		// would want to look at its history.
		"kindlast.core.v1.MemoryService.ListProfileFacts": {
			Method: "GET", Path: "/api/v1/memory/facts",
		},
		"kindlast.core.v1.MemoryService.GetFactHistory": {
			Method: "GET", Path: "/api/v1/memory/facts/{key}/history",
		},
		// A colon verb, matching `audit:export` and `findings:act`. Correcting
		// a fact is not a REST update of a resource at a URL: it closes one row
		// and opens another, and PUT on `/memory/facts/{key}` would describe an
		// overwrite, which is the one thing this surface cannot do.
		"kindlast.core.v1.MemoryService.CorrectFact": {
			Method: "POST", Path: "/api/v1/memory/facts:correct",
		},
		"kindlast.core.v1.MemoryService.ListEvidence": {
			Method: "GET", Path: "/api/v1/memory/evidence",
		},
		// ENT-236. One resource and two custom verbs, because "point this
		// organisation at a hosted provider" and "bring it back" are decisions
		// rather than field edits, and PUT on `/model` would describe them as
		// the same operation with a different body.
		"kindlast.core.v1.ModelService.GetModelSetting": {
			Method: "GET", Path: "/api/v1/model",
		},
		"kindlast.core.v1.ModelService.UseHostedModel": {
			Method: "POST", Path: "/api/v1/model:host",
		},
		"kindlast.core.v1.ModelService.UseBundledModel": {
			Method: "POST", Path: "/api/v1/model:bundle",
		},

		// ENT-212, the first conversation. Singular `session`, for the same
		// reason `UpdateOrganisation` and `GetDashboard` are: it addresses the
		// onboarding of the organisation the header names, not a member of a
		// collection of sessions. An organisation has one interview, and a
		// `/onboarding/sessions/{id}` would invite a client to hold an id and
		// then to ask for somebody else's.
		//
		// `:start` and `:confirm` are colon verbs on that singleton, matching
		// `findings:approve` and `memory/facts:correct`. Neither is a resource:
		// starting is "open one or hand me the open one", and confirming is the
		// single moment answers become facts. POST `/onboarding/sessions` would
		// promise a second session it will not create.
		//
		// Answers are a collection and are posted to one, because that is what
		// they are: an append-only sequence of turns, and a person may answer
		// the same question twice.
		"kindlast.core.v1.OnboardingService.GetOnboardingSession": {
			Method: "GET", Path: "/api/v1/onboarding/session",
		},
		"kindlast.core.v1.OnboardingService.StartOnboarding": {
			Method: "POST", Path: "/api/v1/onboarding/session:start",
		},
		"kindlast.core.v1.OnboardingService.AnswerQuestion": {
			Method: "POST", Path: "/api/v1/onboarding/answers",
		},
		"kindlast.core.v1.OnboardingService.ConfirmProfile": {
			Method: "POST", Path: "/api/v1/onboarding/session:confirm",
		},

		// Connecting a customer's own systems (ENT-231).
		//
		// Still no {org_id}, for the reason every binding above has none: the
		// organisation comes from the header and is checked against
		// membership, so a path that named one would be a second answer to the
		// same question.
		//
		// The reviewable choices here are the three colon verbs, and each is a
		// different argument.
		//
		// `:discover` is an action performed against a third party that stores
		// nothing. There is no resource to GET at
		// `/api/v1/integrations/discover`, and a GET would be wrong anyway
		// because the request carries a credential in its body, which is not a
		// thing to put in a query string that proxies and browser history keep.
		//
		// `:revoke` follows `findings:approve` and `invitations:accept`: one
		// gated transition rather than an arbitrary field change. A PATCH
		// setting `status` would describe a field somebody can put back, and
		// revocation is deliberately terminal.
		//
		// `:fetch` is the live request from the rail. A POST because it makes
		// something happen in somebody else's system and writes an evidence
		// row, and a colon verb because there is no `/fetches` subresource of a
		// connection to create: the fetch log is a collection of the
		// organisation's, filtered by connection, which is why ListFetches
		// sits at `/api/v1/integrations/fetches` rather than under a
		// connection id.
		//
		// `/tools:grant` is the one nested path, and it is nested because a
		// grant is genuinely a property of the connection's tools rather than
		// of the connection. PUT on `/tools` would read as replacing the tool
		// list, which is discovery's job and not the customer's.
		"kindlast.core.v1.IntegrationsService.ListIntegrations": {
			Method: "GET", Path: "/api/v1/integrations",
		},
		"kindlast.core.v1.IntegrationsService.DiscoverIntegration": {
			Method: "POST", Path: "/api/v1/integrations:discover",
		},
		"kindlast.core.v1.IntegrationsService.ConnectIntegration": {
			Method: "POST", Path: "/api/v1/integrations",
		},
		"kindlast.core.v1.IntegrationsService.UpdateToolGrants": {
			Method: "POST", Path: "/api/v1/integrations/{integration_id}/tools:grant",
		},
		"kindlast.core.v1.IntegrationsService.RevokeIntegration": {
			Method: "POST", Path: "/api/v1/integrations/{integration_id}:revoke",
		},
		"kindlast.core.v1.IntegrationsService.ListFetches": {
			Method: "GET", Path: "/api/v1/integrations/fetches",
		},
		"kindlast.core.v1.IntegrationsService.FetchNow": {
			Method: "POST", Path: "/api/v1/integrations/{integration_id}:fetch",
		},

		"kindlast.core.v1.AuditService.ListAuditEntries": {
			Method: "GET", Path: "/api/v1/audit",
		},
		"kindlast.core.v1.AuditService.ExportAuditEntries": {
			Method: "POST", Path: "/api/v1/audit:export",
		},

		"kindlast.core.v1.RecordsService.ListDsars": {
			Method: "GET", Path: "/api/v1/records/dsars",
		},
		"kindlast.core.v1.RecordsService.GetDsar": {
			Method: "GET", Path: "/api/v1/records/dsars/{dsar_id}",
		},

		// The write half. Read these as a group: POST creates on the collection,
		// PATCH replaces on the member, and the one transition that is not a
		// field change gets a custom verb.
		//
		// PATCH rather than PUT even though both replace every field, because
		// the resource has server-owned columns a caller never sends
		// (`source_finding_id`, the timestamps, the computed bands). PUT would
		// promise that the body IS the resource, and it is not.
		//
		// `:respond` is the AIP-136 form, and it is a verb rather than
		// `status: "responded"` on the update because it is one gated
		// transition rather than an arbitrary field change: it asserts the
		// organisation met an Article 12(3) deadline, and it stops a clock.
		"kindlast.core.v1.RecordsService.CreateProcessingActivity": {
			Method: "POST", Path: "/api/v1/records/processing-activities",
		},
		"kindlast.core.v1.RecordsService.UpdateProcessingActivity": {
			Method: "PATCH", Path: "/api/v1/records/processing-activities/{processing_activity_id}",
		},
		"kindlast.core.v1.RecordsService.CreateAiSystem": {
			Method: "POST", Path: "/api/v1/records/ai-systems",
		},
		"kindlast.core.v1.RecordsService.UpdateAiSystem": {
			Method: "PATCH", Path: "/api/v1/records/ai-systems/{ai_system_id}",
		},
		"kindlast.core.v1.RecordsService.LogDsar": {
			Method: "POST", Path: "/api/v1/records/dsars",
		},
		"kindlast.core.v1.RecordsService.MarkDsarResponded": {
			Method: "POST", Path: "/api/v1/records/dsars/{dsar_id}:respond",
		},

		// ENT-226. The trail is a nested COLLECTION, so it is `/trail` and not
		// `:trail`, which is the other side of the rule `:respond` illustrates.
		// A custom verb names an action on a resource; this names a set of
		// subordinate resources that genuinely exist, each with its own id, and
		// AIP-122 spells a subcollection as a path segment.
		//
		// `{dsar_id}` names the request the entries belong to and, as everywhere
		// else on this service, is meaningless without the organisation header.
		//
		// POST and GET on the same path, and no PATCH or DELETE beside them.
		// That absence is the reviewable part: a trail entry is evidence about
		// how a response to a statutory request was assembled, the database
		// refuses an UPDATE with a trigger binding even the migrator, and
		// `kindlast_app` holds no DELETE grant. A binding for either could not
		// be served, so the contract does not offer one. Correcting an entry
		// means appending one that says so.
		"kindlast.core.v1.RecordsService.ListDsarTrail": {
			Method: "GET", Path: "/api/v1/records/dsars/{dsar_id}/trail",
		},
		"kindlast.core.v1.RecordsService.AddDsarTrailEntry": {
			Method: "POST", Path: "/api/v1/records/dsars/{dsar_id}/trail",
		},

		// ENT-203. The only binding outside /api/v1, and the prefix is the
		// reviewable part: /internal/v1 is not reachable through the edge's
		// public routes and is not served to a browser client. The proto
		// package is `platform` rather than `internal` only because Go gives
		// any path segment called `internal` a visibility rule that would make
		// the generated code unimportable; the route keeps the design's name.
		//
		// Still no {org_id}: a sweep names its organisation in the header like
		// everything else.
		"kindlast.platform.v1.SweepService.RunSweep": {
			Method: "POST", Path: "/internal/v1/sweep",
		},
		// ENT-256. The colon verb, as IngestCorpus below: expiring is a custom
		// action over the snoozes, not a subresource. Still no {org_id}, and
		// here for the opposite reason from RunSweep: this one is every
		// organisation at once, by design.
		"kindlast.platform.v1.SweepService.ExpireSnoozes": {
			Method: "POST", Path: "/internal/v1/snoozes:expire",
		},
		// ENT-256, part four. The sweep as two steps (the Analyst alone is
		// the second), and the two lists the schedules ask for: triggers
		// somebody enqueued, and every organisation with a profile. All
		// cross-organisation or header-named as RunSweep is; none takes an
		// {org_id} in the path.
		"kindlast.platform.v1.SweepService.RunAnalyst": {
			Method: "POST", Path: "/internal/v1/sweep:analyse",
		},
		"kindlast.platform.v1.SweepService.ListSweepTriggers": {
			Method: "POST", Path: "/internal/v1/sweep-triggers:pending",
		},
		"kindlast.platform.v1.SweepService.SettleSweepTrigger": {
			Method: "POST", Path: "/internal/v1/sweep-triggers:settle",
		},
		"kindlast.platform.v1.SweepService.ListSweepTargets": {
			Method: "POST", Path: "/internal/v1/sweep-targets:list",
		},
		// ENT-256, part three. The outbox's delivery half, three custom
		// actions over the messages, all cross-organisation by design and all
		// on the internal prefix: what is pending, deliver this one, reclaim
		// what no longer needs keeping. POST for the list too, because the
		// convention on this surface is one verb and a body, and a GET with
		// a query string would be the only one of its kind here.
		"kindlast.platform.v1.DeliveryService.ListUndelivered": {
			Method: "POST", Path: "/internal/v1/messages:pending",
		},
		"kindlast.platform.v1.DeliveryService.DeliverMessage": {
			Method: "POST", Path: "/internal/v1/messages:deliver",
		},
		"kindlast.platform.v1.DeliveryService.ReclaimMessages": {
			Method: "POST", Path: "/internal/v1/messages:reclaim",
		},
		// The doorbell path's three verbs, same shape: the workflow plans,
		// sends to whoever is due, and settles the row when nobody is left.
		"kindlast.platform.v1.DeliveryService.PlanNotification": {
			Method: "POST", Path: "/internal/v1/notifications:plan",
		},
		"kindlast.platform.v1.DeliveryService.NotifyRecipients": {
			Method: "POST", Path: "/internal/v1/notifications:notify",
		},
		"kindlast.platform.v1.DeliveryService.SettleNotification": {
			Method: "POST", Path: "/internal/v1/notifications:settle",
		},
		// ENT-256, part five. One model call, through core-api, so the
		// organisation's key never leaves Go. The prompt crosses here as
		// data; the route and the credential are resolved on the far side.
		"kindlast.platform.v1.CompletionService.Complete": {
			Method: "POST", Path: "/internal/v1/completions",
		},
		// The narration chain's load and persist steps (ENT-256, part five):
		// what the Go worker calls either side of the Python activity.
		"kindlast.platform.v1.NarrativeService.NextFindingToNarrate": {
			Method: "POST", Path: "/internal/v1/findings:next-to-narrate",
		},
		"kindlast.platform.v1.NarrativeService.RecordNarrative": {
			Method: "POST", Path: "/internal/v1/findings:record-narrative",
		},
		// The Executor (ENT-271): what the workflow calls to create the
		// record an approved finding asked for. Cross-organisation like the
		// rest of this surface; the organisation comes from the job row.
		"kindlast.platform.v1.ExecutorService.ListPendingJobs": {
			Method: "POST", Path: "/internal/v1/executor-jobs:pending",
		},
		"kindlast.platform.v1.ExecutorService.ExecuteJob": {
			Method: "POST", Path: "/internal/v1/executor-jobs:execute",
		},
		// The scheduled fetch (ENT-279): what the relay lists and what one
		// fetch workflow calls. Cross-organisation like the Executor, and for
		// the same reason: the organisation and the consenting person come
		// from the connection's own rows, never from the caller.
		"kindlast.platform.v1.FetchService.ListFetchTargets": {
			Method: "POST", Path: "/internal/v1/fetch-targets:due",
		},
		"kindlast.platform.v1.FetchService.RunScheduledFetch": {
			Method: "POST", Path: "/internal/v1/fetches:run",
		},
		// The Watcher's own surface (ENT-258): what it reads, and the one
		// thing it writes. `/signals` is a collection because raising one is
		// creating a signal; the context is a custom action because it is a
		// read assembled for one caller rather than a resource.
		"kindlast.platform.v1.WatcherService.WatcherContext": {
			Method: "POST", Path: "/internal/v1/watcher:context",
		},
		"kindlast.platform.v1.WatcherService.RaiseSignal": {
			Method: "POST", Path: "/internal/v1/signals",
		},
		// And what one connection has already reported (ENT-274). A custom
		// action on the same collective noun as the context, for the same
		// reason: it is a read assembled for one caller out of two tables,
		// not a resource anybody can address.
		"kindlast.platform.v1.WatcherService.ReadEvidence": {
			Method: "POST", Path: "/internal/v1/watcher:evidence",
		},
		// And the ask for a fetch (ENT-279). A custom action, not a
		// collection: what it creates is queued work with no addressable
		// resource behind it, and the acknowledgement is the answer, not a
		// representation of anything.
		"kindlast.platform.v1.WatcherService.RequestFetch": {
			Method: "POST", Path: "/internal/v1/watcher:request-fetch",
		},

		// The Hands' surface (ENT-261): what approving a finding will do, and
		// the plan it prepares. Both are custom actions rather than resources,
		// because neither creates one: the first runs an agent and the second
		// writes a proposal onto a finding that already exists.
		//
		// NEITHER PATH APPROVES ANYTHING, and the absence is the point of the
		// whole surface. Approving is `/api/v1/findings/{finding_id}:approve`
		// on `findings:act`, which only a human's token carries.
		"kindlast.platform.v1.HandsService.ExplainApproval": {
			Method: "POST", Path: "/internal/v1/hands:explain",
		},
		"kindlast.platform.v1.HandsService.PrepareRecord": {
			Method: "POST", Path: "/internal/v1/hands:prepare",
		},

		// Writing the corpus (ENT-207). Also on /internal/v1, and it has to be:
		// a request from the console that could change the law would make the
		// product's central claim, that a customer can check a finding against
		// the regulation, mean nothing.
		//
		// The colon verb rather than `/internal/v1/corpus`, because this is an
		// action rather than a resource. There is no corpus to PUT: the pack is
		// a snapshot to apply, and applying it is an upsert over ten tables.
		//
		// And no {org_id} for the strongest available reason: the corpus has no
		// `org_id` at all. It is the same law for every customer, so there is no
		// organisation to name in a path or a header.
		"kindlast.platform.v1.IngestService.IngestCorpus": {
			Method: "POST", Path: "/internal/v1/corpus:ingest",
		},

		// A plural collection and a plain POST, unlike its neighbour above,
		// because this one really is creating a resource: one run happened and
		// one row records it. The corpus RPC is a colon verb because applying a
		// pack is an action over ten tables rather than the creation of a
		// thing.
		//
		// No {org_id} in the path, and for a different reason from the corpus.
		// The corpus has no organisation at all; a run has exactly one, and it
		// travels in the body because this caller holds no session to derive an
		// active organisation from. See the RPC's comment for why that is safe
		// and what would stop it being so.
		"kindlast.platform.v1.IngestService.RecordAgentRun": {
			Method: "POST", Path: "/internal/v1/agent-runs",
		},

		// What a machine fetched from a customer's own system (ENT-231).
		//
		// A plural collection and a plain POST, like the agent runs above and
		// unlike the corpus verb beside them, because this really is creating a
		// resource: one fetch happened and one row records it.
		//
		// On /internal/v1 and it has to be. A person's live fetch from the
		// console goes to `/api/v1/integrations/{id}:fetch` instead, because a
		// user token can never carry an `internal:*` scope; the two paths write
		// the same rows and neither is reachable by the other's caller. See the
		// RPC's comment for why that asymmetry is deliberate.
		//
		// No {org_id}, and for the same reason RecordAgentRun has none: a fetch
		// has exactly one organisation and it travels in the body, because this
		// caller holds no session to derive an active organisation from.
		"kindlast.platform.v1.IngestService.IngestEvidence": {
			Method: "POST", Path: "/internal/v1/evidence",
		},

		// A colon verb over the findings collection, because narrating is an
		// action across the findings that have no narrative yet rather than the
		// creation of a thing. Same reasoning as the corpus RPC two entries up.
		//
		// The organisation travels in the body for the same reason RecordAgentRun's
		// does: this caller is a service principal with no session, so there is
		// no active organisation to derive.
		//
		// This binding went unpinned until the registry bug was fixed. The test
		// walks server.Services(), so a service missing from the registry is
		// invisible here too, and neither guard fired. That is why the registry
		// now has a completeness test of its own.
		"kindlast.platform.v1.NarrativeService.NarrateFindings": {
			Method: "POST", Path: "/internal/v1/findings:narrate",
		},
	}

	got := map[string]httprule.Binding{}
	for _, service := range server.Services() {
		bindings, _ := httprule.OfService(service)
		for name, binding := range bindings {
			got[name] = binding
		}
	}

	for name, expected := range want {
		actual, ok := got[name]
		if !ok {
			t.Errorf("%s declares no binding", name)
			continue
		}
		if actual != expected {
			t.Errorf("%s binds %s, want %s", name, actual, expected)
		}
	}

	// Every served method must appear above. A new RPC that nobody added here
	// is a new REST path nobody reviewed, which is the whole point of pinning
	// the contract before strangers depend on it.
	for name := range got {
		if _, expected := want[name]; !expected {
			t.Errorf("%s declares a binding this test does not pin; add it, "+
				"so the path is reviewed rather than merely generated", name)
		}
	}
}

// The guard is only worth having if it can fail. This strips the options off
// the real service descriptors and asserts the same walk reports every method,
// so the check's ability to fail is itself under test rather than resting on
// someone having tried it once by hand.
func TestTheBindingCheckCanActuallyFail(t *testing.T) {
	for _, service := range server.Services() {
		if service.Methods().Len() == 0 {
			continue
		}

		stripped := withoutMethodOptionsForBindings(t, service)
		_, undeclared := httprule.OfService(stripped)

		if len(undeclared) != stripped.Methods().Len() {
			t.Fatalf("%s: stripped of options, got %d offenders for %d methods; "+
				"the check is not looking at what it claims to",
				service.FullName(), len(undeclared), stripped.Methods().Len())
		}
	}
}

func withoutMethodOptionsForBindings(t *testing.T, service protoreflect.ServiceDescriptor) protoreflect.ServiceDescriptor {
	t.Helper()

	file := protodesc.ToFileDescriptorProto(service.ParentFile())
	for _, svc := range file.Service {
		for _, method := range svc.Method {
			method.Options = nil
		}
	}

	// The real file imports google/api/annotations.proto, so rebuilding it
	// needs a resolver that can find those dependencies. GlobalFiles has them,
	// because the generated code registered them on import.
	rebuilt, err := protodesc.NewFile(file, protoregistry.GlobalFiles)
	if err != nil {
		t.Fatalf("rebuilding %s without method options: %v", service.FullName(), err)
	}

	return rebuilt.Services().ByName(service.Name())
}
