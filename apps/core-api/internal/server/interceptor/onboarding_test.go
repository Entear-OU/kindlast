package interceptor_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	memoryservice "github.com/Entear-OU/kindlast/apps/core-api/internal/service/memory"
	onboardingservice "github.com/Entear-OU/kindlast/apps/core-api/internal/service/onboarding"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/service/session"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
	corev1 "github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1"
	"github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1/corev1connect"
)

// Onboarding through the production chain (ENT-212).
//
// # WHY THIS IS NOT A HANDLER TEST WITH A FAKE STORE
//
// The failures worth catching here are all at seams a fake removes. A service
// registered on the mux but missing from `server.Services()` is default-denied
// at runtime with "declares no required scope" while every unit test around it
// stays green, which is exactly what happened to NarrativeService in ENT-245.
// A token holding the wrong scope is refused before any handler runs. And the
// facts onboarding writes have to arrive in the same table `MemoryService`
// reads, which is the whole claim of the change and is unprovable against a
// store nobody wrote.
//
// So: real signed tokens, the real interceptor chain built from the real
// registry, and a real Postgres enforcing the policies.
//
// Needs the compose stack. Skips locally without it and fails in CI, matching
// every other suite here.

// Read and write, plus the memory read that proves where the answers landed.
const onboardingScopes = "openid profile onboarding:read onboarding:write memory:read"

func buildOnboardingChain(t *testing.T, a *authServer) (
	corev1connect.SessionServiceClient,
	corev1connect.OnboardingServiceClient,
	corev1connect.MemoryServiceClient,
) {
	t.Helper()

	live := requireStack(t, a.server.URL)
	// FROM THE REAL REGISTRY, WHICH IS HALF THE POINT OF THIS FILE. If
	// `onboarding.proto` were missing from `server.Services()`, this table
	// would not carry its four procedures and every call below would be refused
	// with "declares no required scope" rather than reaching a handler.
	scopes := realScopes(t)
	chain := connect.WithInterceptors(
		interceptor.Auth(a.verifier(t)),
		interceptor.JTI(live.revocations),
		scopes.Interceptor(),
		interceptor.Tenancy(tenantOpener{live.store}),
	)

	mux := http.NewServeMux()
	mux.Handle(corev1connect.NewSessionServiceHandler(session.New(a.profiles(t)), chain))
	mux.Handle(corev1connect.NewOnboardingServiceHandler(onboardingservice.New(), chain))
	mux.Handle(corev1connect.NewMemoryServiceHandler(memoryservice.New(), chain))

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return corev1connect.NewSessionServiceClient(server.Client(), server.URL),
		corev1connect.NewOnboardingServiceClient(server.Client(), server.URL),
		corev1connect.NewMemoryServiceClient(server.Client(), server.URL)
}

// onboarder provisions a person who can be interviewed.
func onboarder(
	t *testing.T, a *authServer, client corev1connect.SessionServiceClient, label string,
) member {
	t.Helper()

	claim := fmt.Sprintf("onb-%s-%d", label, time.Now().UnixNano())
	forget(t, a.server.URL, claim)
	t.Cleanup(func() { forget(t, a.server.URL, claim) })

	a.serveUserInfo(claim, map[string]any{
		"name": label, "email": label + "@example.invalid",
	})

	headers := map[string]string{
		"Authorization": "Bearer " + a.tokenWithClaims(t,
			mapClaims(a, claim, map[string]any{"scope": onboardingScopes})),
	}

	me, err := meCall(t, client, headers)
	if err != nil {
		t.Fatalf("provisioning %s: %v", label, err)
	}

	m := member{
		claim:   claim,
		orgID:   me.GetMemberships()[0].GetOrgId(),
		orgSlug: me.GetMemberships()[0].GetOrgSlug(),
		headers: headers,
	}
	m.headers[interceptor.OrgHeader] = m.orgID
	return m
}

// answerText is what a person taps for each question.
//
// Written out rather than generated, because the point is that these are the
// answers somebody gave and the assertions below are about what they became.
// Since ENT-254 a list answer is the comma-joined tokens of the options the
// question offered, and anything else is refused.
var answerText = map[corev1.ProfileFactKey]string{
	corev1.ProfileFactKey_PROFILE_FACT_KEY_DATA_CATEGORIES:        "contact_details,payment",
	corev1.ProfileFactKey_PROFILE_FACT_KEY_LAWFUL_BASES:           "consent,contract",
	corev1.ProfileFactKey_PROFILE_FACT_KEY_VENDOR_LIST:            "hosting,payments",
	corev1.ProfileFactKey_PROFILE_FACT_KEY_TRANSFERS_OUTSIDE_EU:   "yes",
	corev1.ProfileFactKey_PROFILE_FACT_KEY_TRANSFER_DESTINATIONS:  "united_states",
	corev1.ProfileFactKey_PROFILE_FACT_KEY_HIGH_RISK_PROCESSING:   "no",
	corev1.ProfileFactKey_PROFILE_FACT_KEY_LARGE_SCALE_MONITORING: "no",
	corev1.ProfileFactKey_PROFILE_FACT_KEY_HAS_ROPA:               "unsure",
	corev1.ProfileFactKey_PROFILE_FACT_KEY_HAS_DPO:                "no",
	corev1.ProfileFactKey_PROFILE_FACT_KEY_AI_SYSTEMS:             "assistants",
	corev1.ProfileFactKey_PROFILE_FACT_KEY_HIGH_RISK_AI_SYSTEM:    "no",
}

func TestAPersonJustProvisionedCanCompleteOnboarding(t *testing.T) {
	auth := newAuthServer(t)
	sessions, onboarding, memory := buildOnboardingChain(t, auth)
	me := onboarder(t, auth, sessions, "ada")

	// THE TENANCY SUBTLETY ENT-212 NAMES. This person's organisation was
	// created by the `/me` call three lines up and holds nothing at all. The
	// interview has to work in that state, without a second sign-in.
	before, err := onboarding.GetOnboardingSession(t.Context(),
		withHeaders(connect.NewRequest(&corev1.GetOnboardingSessionRequest{}), me.headers))
	if err != nil {
		t.Fatalf("reading the state of a brand new organisation: %v", err)
	}
	if before.Msg.GetState().GetProfileExists() {
		t.Fatal("a freshly provisioned organisation claims a compliance profile")
	}
	if before.Msg.GetState().GetSessionId() != "" {
		t.Fatal("reading the state opened a session, which a read must not do")
	}

	started, err := onboarding.StartOnboarding(t.Context(),
		withHeaders(connect.NewRequest(&corev1.StartOnboardingRequest{}), me.headers))
	if err != nil {
		t.Fatalf("starting onboarding: %v", err)
	}
	if !started.Msg.GetCreated() {
		t.Fatal("starting a first interview reported that one already existed")
	}
	state := started.Msg.GetState()
	if state.GetNextQuestion() == nil {
		t.Fatal("the interview opened with no question")
	}

	// Every question offers exactly what the server accepts. A console
	// rendering its own list would produce answers refused at the wire, and
	// this is the only place both halves are on screen at once (ENT-254).
	if question := state.GetNextQuestion(); question.GetShape() == corev1.AnswerShape_ANSWER_SHAPE_LIST &&
		len(question.GetOptions()) == 0 {
		t.Fatal("a list question arrived with nothing to pick from")
	}

	// Answer whatever is asked, until nothing is, checking after the first one
	// that the answer was believed rather than parked.
	first := true
	for state.GetNextQuestion() != nil {
		key := state.GetNextQuestion().GetKey()
		text, known := answerText[key]
		if !known {
			t.Fatalf("the interview asked about %v and this test has no answer for it", key)
		}
		answered, err := onboarding.AnswerQuestion(t.Context(),
			withHeaders(connect.NewRequest(&corev1.AnswerQuestionRequest{
				Key: key, Answer: text,
			}), me.headers))
		if err != nil {
			t.Fatalf("answering %v with %q: %v", key, text, err)
		}
		state = answered.Msg.GetState()

		if first {
			// ENT-254: SAVED AS IT WAS GIVEN. One answer in, and the fact is
			// already in the organisation memory, read through the service the
			// console's memory page uses.
			saved, err := memory.ListProfileFacts(t.Context(),
				withHeaders(connect.NewRequest(&corev1.ListProfileFactsRequest{}), me.headers))
			if err != nil {
				t.Fatalf("reading the memory after one answer: %v", err)
			}
			if len(saved.Msg.GetFacts()) != 1 {
				t.Fatalf("%d facts after one answer, want 1: an answer is believed as it is given",
					len(saved.Msg.GetFacts()))
			}
			// And the profile the Watcher reads is NOT there yet, so a
			// half-described organisation produces no findings and every
			// authenticated route goes on routing this person back here.
			if state.GetProfileExists() {
				t.Fatal("a compliance profile existed after one answer of eleven")
			}
			first = false
		}
	}

	if !state.GetReadyToConfirm() {
		t.Fatal("every question is answered and the interview does not think so")
	}
	// AND THE LAST ANSWER FINISHED IT, with no confirmation call. That is the
	// whole of ENT-254's "no separate confirmation step": there is no screen to
	// skip past because there is no screen.
	if !state.GetProfileExists() {
		t.Fatal("the last answer did not write the compliance profile")
	}
	if state.GetStatus() != "completed" {
		t.Fatalf("the interview is %q after its last answer, want completed", state.GetStatus())
	}

	// AND THEY ARRIVED IN THE ORGANISATION MEMORY, not in a parallel profile.
	// This is the read the console's memory page makes, through a different
	// service, and it is the whole claim of ENT-212.
	facts, err := memory.ListProfileFacts(t.Context(),
		withHeaders(connect.NewRequest(&corev1.ListProfileFactsRequest{}), me.headers))
	if err != nil {
		t.Fatalf("reading the profile after the interview: %v", err)
	}
	byKey := map[corev1.ProfileFactKey]*corev1.ProfileFact{}
	for _, fact := range facts.Msg.GetFacts() {
		byKey[fact.GetKey()] = fact
		if fact.GetSource() != "onboarding" {
			t.Errorf("fact %v is sourced to %q, want onboarding", fact.GetKey(), fact.GetSource())
		}
		if fact.GetRecordedBy() == "" {
			t.Errorf("fact %v records nobody as having said it", fact.GetKey())
		}
	}
	if len(byKey) != len(answerText) {
		t.Fatalf("%d facts recorded, want %d", len(byKey), len(answerText))
	}

	bases := byKey[corev1.ProfileFactKey_PROFILE_FACT_KEY_LAWFUL_BASES]
	if got := bases.GetValue().GetList().GetValues(); len(got) != 2 ||
		got[0] != "consent" || got[1] != "contract" {
		t.Fatalf("%q became %v", answerText[corev1.ProfileFactKey_PROFILE_FACT_KEY_LAWFUL_BASES], got)
	}
	// Unsure survives as unsure rather than collapsing to no, which is a
	// different claim about the same organisation and a different set of
	// findings.
	if byKey[corev1.ProfileFactKey_PROFILE_FACT_KEY_HAS_ROPA].GetValue().GetTriState() !=
		corev1.TriState_TRI_STATE_UNSURE {
		t.Fatal("an unsure answer did not survive as unsure")
	}
}

func TestAnAnswerTheProductCannotReadIsRefusedRatherThanInterpreted(t *testing.T) {
	auth := newAuthServer(t)
	sessions, onboarding, _ := buildOnboardingChain(t, auth)
	me := onboarder(t, auth, sessions, "bob")

	if _, err := onboarding.StartOnboarding(t.Context(),
		withHeaders(connect.NewRequest(&corev1.StartOnboardingRequest{}), me.headers)); err != nil {
		t.Fatalf("starting onboarding: %v", err)
	}

	// THE REFUSAL THE WHOLE DESIGN RESTS ON. Since ENT-254 every list answer is
	// a token out of a closed set, and both evaluators that decide which
	// obligations apply match on those tokens. A value outside the set would be
	// a fact nothing will ever match, which is an obligation quietly ceasing to
	// apply, and that is the failure ENT-246 spent a whole issue undoing.
	_, err := onboarding.AnswerQuestion(t.Context(),
		withHeaders(connect.NewRequest(&corev1.AnswerQuestionRequest{
			Key:    corev1.ProfileFactKey_PROFILE_FACT_KEY_VENDOR_LIST,
			Answer: "Stripe and a few others, I would have to check",
		}), me.headers))
	if err == nil {
		t.Fatal("prose was accepted where a closed set of tokens is offered")
	}
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("refused with %v, want invalid_argument: a bad answer is the "+
			"caller's problem and should be reported as one", connect.CodeOf(err))
	}

	// And the same for a tri-state, where the wrong answer is the plausible one.
	_, err = onboarding.AnswerQuestion(t.Context(),
		withHeaders(connect.NewRequest(&corev1.AnswerQuestionRequest{
			Key:    corev1.ProfileFactKey_PROFILE_FACT_KEY_HAS_ROPA,
			Answer: "probably, I think Dave made one",
		}), me.headers))
	if err == nil {
		t.Fatal("a maybe was accepted as an answer about a record of processing activities")
	}
}

func TestSkippingAQuestionLeavesTheFactAbsentRatherThanGuessed(t *testing.T) {
	auth := newAuthServer(t)
	sessions, onboarding, memory := buildOnboardingChain(t, auth)
	me := onboarder(t, auth, sessions, "cleo")

	started, err := onboarding.StartOnboarding(t.Context(),
		withHeaders(connect.NewRequest(&corev1.StartOnboardingRequest{}), me.headers))
	if err != nil {
		t.Fatalf("starting onboarding: %v", err)
	}

	state := started.Msg.GetState()
	skipped := 0
	for state.GetNextQuestion() != nil {
		key := state.GetNextQuestion().GetKey()
		request := &corev1.AnswerQuestionRequest{Key: key}
		// Skip everything except the one fact this test then looks for.
		if key == corev1.ProfileFactKey_PROFILE_FACT_KEY_HAS_ROPA {
			request.Answer = "yes"
		} else {
			request.Skip = true
			skipped++
		}
		answered, err := onboarding.AnswerQuestion(t.Context(),
			withHeaders(connect.NewRequest(request), me.headers))
		if err != nil {
			t.Fatalf("answering %v: %v", key, err)
		}
		state = answered.Msg.GetState()
	}
	if skipped == 0 {
		t.Fatal("nothing was skipped, so this test proves nothing")
	}

	facts, err := memory.ListProfileFacts(t.Context(),
		withHeaders(connect.NewRequest(&corev1.ListProfileFactsRequest{}), me.headers))
	if err != nil {
		t.Fatalf("reading the profile: %v", err)
	}
	// One fact, not eleven with ten placeholders. A skipped question produces
	// no fact at all, which is what "left empty rather than guessed" means when
	// it is a property of the code rather than a line in a prompt.
	if len(facts.Msg.GetFacts()) != 1 {
		t.Fatalf("%d facts recorded after ten skips, want 1", len(facts.Msg.GetFacts()))
	}
	if facts.Msg.GetFacts()[0].GetKey() != corev1.ProfileFactKey_PROFILE_FACT_KEY_HAS_ROPA {
		t.Fatalf("the one recorded fact is %v", facts.Msg.GetFacts()[0].GetKey())
	}
}

func TestSkippingEverythingWritesNoProfileToReasonFrom(t *testing.T) {
	// The interview a person walked out of. Ten skips leave no fact, and a
	// compliance profile built from a script's worth of defaults nobody stated
	// would have the Watcher produce findings the customer never gave it
	// grounds for. So the last skip finishes nothing: `profile_exists` stays
	// false and every authenticated route goes on routing them back here.
	auth := newAuthServer(t)
	sessions, onboarding, memory := buildOnboardingChain(t, auth)
	me := onboarder(t, auth, sessions, "dara")

	started, err := onboarding.StartOnboarding(t.Context(),
		withHeaders(connect.NewRequest(&corev1.StartOnboardingRequest{}), me.headers))
	if err != nil {
		t.Fatalf("starting onboarding: %v", err)
	}

	state := started.Msg.GetState()
	for state.GetNextQuestion() != nil {
		answered, err := onboarding.AnswerQuestion(t.Context(),
			withHeaders(connect.NewRequest(&corev1.AnswerQuestionRequest{
				Key: state.GetNextQuestion().GetKey(), Skip: true,
			}), me.headers))
		if err != nil {
			t.Fatalf("skipping: %v", err)
		}
		state = answered.Msg.GetState()
	}

	if state.GetProfileExists() {
		t.Fatal("skipping every question produced a compliance profile")
	}
	facts, err := memory.ListProfileFacts(t.Context(),
		withHeaders(connect.NewRequest(&corev1.ListProfileFactsRequest{}), me.headers))
	if err != nil {
		t.Fatalf("reading the profile: %v", err)
	}
	if len(facts.Msg.GetFacts()) != 0 {
		t.Fatalf("%d facts recorded after skipping everything", len(facts.Msg.GetFacts()))
	}
}

func TestOneOrganisationsAnswersAreInvisibleToAnother(t *testing.T) {
	auth := newAuthServer(t)
	sessions, onboarding, _ := buildOnboardingChain(t, auth)

	ada := onboarder(t, auth, sessions, "ada-tenancy")
	bob := onboarder(t, auth, sessions, "bob-tenancy")

	started, err := onboarding.StartOnboarding(t.Context(),
		withHeaders(connect.NewRequest(&corev1.StartOnboardingRequest{}), ada.headers))
	if err != nil {
		t.Fatalf("starting Ada's interview: %v", err)
	}
	if _, err := onboarding.AnswerQuestion(t.Context(),
		withHeaders(connect.NewRequest(&corev1.AnswerQuestionRequest{
			Key:    started.Msg.GetState().GetNextQuestion().GetKey(),
			Answer: answerText[started.Msg.GetState().GetNextQuestion().GetKey()],
		}), ada.headers)); err != nil {
		t.Fatalf("answering as Ada: %v", err)
	}

	// Bob asks for his own state, holding a valid token, in his own
	// organisation. He must see an interview that has not started, not Ada's.
	bobsView, err := onboarding.GetOnboardingSession(t.Context(),
		withHeaders(connect.NewRequest(&corev1.GetOnboardingSessionRequest{}), bob.headers))
	if err != nil {
		t.Fatalf("reading Bob's state: %v", err)
	}
	if bobsView.Msg.GetState().GetSessionId() != "" {
		t.Fatal("Bob can see an onboarding session, and he has not started one")
	}
	if len(bobsView.Msg.GetState().GetTranscript()) != 0 {
		t.Fatal("Bob can read turns of somebody else's interview")
	}

	// The control: Ada can still see her own, so the emptiness above is
	// isolation rather than the fixture never landing.
	adasView, err := onboarding.GetOnboardingSession(t.Context(),
		withHeaders(connect.NewRequest(&corev1.GetOnboardingSessionRequest{}), ada.headers))
	if err != nil {
		t.Fatalf("reading Ada's state: %v", err)
	}
	if len(adasView.Msg.GetState().GetTranscript()) == 0 {
		t.Fatal("Ada cannot see her own interview")
	}
}

func TestATokenWithoutTheOnboardingScopeIsRefused(t *testing.T) {
	auth := newAuthServer(t)
	sessions, onboarding, _ := buildOnboardingChain(t, auth)
	me := onboarder(t, auth, sessions, "narrow")

	// A token carrying everything except onboarding. The RPC is reachable, the
	// person is a member, and the answer must still be no: authority is the
	// scope interceptor rather than the handler.
	me.headers["Authorization"] = "Bearer " + auth.tokenWithClaims(t,
		mapClaims(auth, me.claim, map[string]any{"scope": "openid profile memory:read"}))

	if _, err := onboarding.StartOnboarding(t.Context(),
		withHeaders(connect.NewRequest(&corev1.StartOnboardingRequest{}), me.headers)); err == nil {
		t.Fatal("a token with no onboarding scope started an interview")
	} else if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("refused with %v, want permission_denied", connect.CodeOf(err))
	}

	// And the read is separately scoped, so holding the write does not imply it
	// and holding neither does not reach it.
	if _, err := onboarding.GetOnboardingSession(t.Context(),
		withHeaders(connect.NewRequest(&corev1.GetOnboardingSessionRequest{}), me.headers)); err == nil {
		t.Fatal("a token with no onboarding scope read the interview's state")
	}
}

// TestConfirmingOnboardingThroughTheRealChainLeavesASweepForTheWorker is the
// seam AGENTS.md asks to be driven once end to end rather than trusted from
// unit coverage of each half (00035, ENT-256 part four).
//
// The postgres package already proves ConfirmOnboarding writes the trigger row
// and the agent pool lists, runs and settles it, each against a real
// transaction. Neither of those calls goes through `ConfirmProfile`, the
// tenancy interceptor, or the scope gate, and ENT-245 is the standing reminder
// of what that gap can hide: a service correctly implemented and unreachable
// in production, with every narrower test green. So this one calls the RPC the
// console actually calls, through the real registry and the real interceptor
// chain, and only then reaches for the agent pool to prove the row it left
// behind is the row the worker's relay would list, and can be run and settled
// the way the workflow does it.
func TestConfirmingOnboardingThroughTheRealChainLeavesASweepForTheWorker(t *testing.T) {
	auth := newAuthServer(t)
	sessions, onboardingClient, _ := buildOnboardingChain(t, auth)
	agentPool := requireAgentPool(t)

	me := onboarder(t, auth, sessions, "sweep-trigger")

	state, err := onboardingClient.StartOnboarding(t.Context(),
		withHeaders(connect.NewRequest(&corev1.StartOnboardingRequest{}), me.headers))
	if err != nil {
		t.Fatalf("starting onboarding: %v", err)
	}
	next := state.Msg.GetState()
	for next.GetNextQuestion() != nil {
		key := next.GetNextQuestion().GetKey()
		text, known := answerText[key]
		if !known {
			t.Fatalf("the interview asked about %v and this test has no answer for it", key)
		}
		answered, err := onboardingClient.AnswerQuestion(t.Context(),
			withHeaders(connect.NewRequest(&corev1.AnswerQuestionRequest{
				Key: key, Answer: text,
			}), me.headers))
		if err != nil {
			t.Fatalf("answering %v: %v", key, err)
		}
		next = answered.Msg.GetState()
	}

	// No confirmation call, because ENT-254 removed the step: the last answer
	// wrote the profile and left the trigger. That is the property this test
	// now carries end to end.
	if !next.GetProfileExists() {
		t.Fatal("the last answer through the real chain produced no profile")
	}

	// What the relay asks, on the agent pool, across the same connection
	// boundary the worker crosses.
	triggers, err := agentPool.PendingSweepTriggers(t.Context(), 1000)
	if err != nil {
		t.Fatalf("listing sweep triggers: %v", err)
	}
	var mine *postgres.SweepTrigger
	for i := range triggers {
		if triggers[i].OrgID == me.orgID {
			mine = &triggers[i]
		}
	}
	if mine == nil {
		t.Fatalf("the sweep triggered by confirming %s's onboarding was not listed for the worker", me.orgID)
	}

	// What the workflow does with it.
	if _, err := agentPool.RunSweep(t.Context(), mine.OrgID, true); err != nil {
		t.Fatalf("the watcher: %v", err)
	}
	if _, err := agentPool.RunAnalyst(t.Context(), mine.OrgID); err != nil {
		t.Fatalf("the analyst: %v", err)
	}
	if settled, err := agentPool.SettleSweepTrigger(t.Context(), mine.ID, nil); err != nil || !settled {
		t.Fatalf("settling: settled=%v err=%v", settled, err)
	}

	var status string
	if err := seeder(t).QueryRow(t.Context(),
		`select status from sweep_triggers where id = $1::uuid`, mine.ID).Scan(&status); err != nil {
		t.Fatalf("reading the sweep trigger: %v", err)
	}
	if status != "done" {
		t.Fatalf("status = %q after the workflow's steps, want done", status)
	}
}
