// Package onboarding serves OnboardingService: the first conversation
// (ENT-212, §2, §24 step 6, §26.5).
//
// # WHAT THIS SURFACE IS FOR
//
// It collects what an organisation is, one typed answer at a time, and records
// each answer as a profile fact with `source = 'onboarding'` at the moment it
// is given. It is the organisation memory's first feeder: the same table the
// memory page reads, the same history, the same provenance, the same
// `recorded_by`.
//
// # THE CONFIRMATION STEP IS GONE (ENT-254)
//
// ENT-212 held answers in the transcript until `ConfirmProfile`. ENT-254 moved
// the readiness assessment into this flow and ruled that answers save as they
// are given, with no review-then-commit screen, because that is what "easy"
// means for somebody who has an account and no compliance profile yet.
//
// The property the step existed for survives in a better place. It was there so
// nothing was believed that the person had not read back; what they read back
// now is the parsed value beside their own answer, at the moment they give it,
// which is where a wrong reading is cheapest to notice. And every fact is
// correctable afterwards with its history intact, which the confirmation screen
// never made true.
//
// What still happens once, at the end, is the `compliance_profiles` projection,
// the completed status and the sweep trigger. See `finishIfDone` for why those
// three cannot be per-answer.
//
// # STREAMING IS NOT HERE, AND THAT IS A DECISION RATHER THAN AN OMISSION
//
// ENT-212 is titled "onboarding, streaming last", and §25 asks whether a token
// stream should pass through core-api or go straight to Intelligence. The
// routing question is answered in the proto and is not in doubt: through
// core-api, because no user token may reach Intelligence, because Intelligence
// holds no tenancy GUCs and could not check who is asking, and because it holds
// no database handle, so a browser-direct stream would produce a conversation
// nothing persisted.
//
// What is deferred is the stream itself, and the model-drafted question with
// it. Three things made that the right call for this change:
//
//   - The interview does not need a model to be correct. Every value in the
//     profile comes from a typed answer parsed by Go against a closed
//     vocabulary, so a model could only ever rephrase the question. That is
//     worth having and it is not worth blocking the surface on.
//   - It is required to work without one anyway. ENT-212 asks that an instance
//     with no model provider "degrades to a form rather than failing", so the
//     scripted interview has to exist regardless, and a second model-driven
//     path would be a second thing to keep correct.
//   - Local inference is slow enough to change the design. A 2B model on CPU
//     takes minutes per response, so a question drafted per turn is a minutes-
//     long wait between "what does your company do" and "and whose data is it",
//     and a stream through a proxy is exactly what §0.1 flags as the thing that
//     breaks first. Shipping that badly would be worse than shipping this.
//
// The shape here does not have to change to add it: an assistant turn is
// already a row, so a model that drafts a question writes the same row this
// does.
package onboarding

import (
	"context"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"

	memorydomain "github.com/Entear-OU/kindlast/apps/core-api/internal/domain/memory"
	domain "github.com/Entear-OU/kindlast/apps/core-api/internal/domain/onboarding"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	memoryservice "github.com/Entear-OU/kindlast/apps/core-api/internal/service/memory"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
	corev1 "github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1"
)

// greeting is the opening line, written once into the transcript.
//
// One warm sentence and then the first question, which is what
// `system-prompt.ts` settled at `db0bf83` and is a product decision that was
// reviewed once already. It is a row rather than console copy so the
// conversation a customer reads back is complete.
const greeting = "Hello. I am going to ask eleven short questions so Kindlast " +
	"knows what your organisation actually does. Every one is a tap, your " +
	"answers are saved as you give them, and if you cannot answer one you can " +
	"pass on it and we will move on."

// store is what these handlers need of the request's transaction, declared
// where it is used (§21.6).
type store interface {
	OnboardingSession(ctx context.Context) (domain.Session, error)
	StartOnboardingSession(ctx context.Context) (domain.Session, bool, error)
	OnboardingTranscript(ctx context.Context, sessionID string) ([]domain.Turn, error)
	AppendOnboardingTurn(ctx context.Context, sessionID, role, content, factKey, factValueJSON string) (domain.Turn, error)
	// The same close-then-insert path a human correction takes. Onboarding is
	// the organisation memory's first feeder, so its writes are the same
	// writes, with the same provenance and the same history (ENT-254).
	CorrectFact(ctx context.Context, key, valueJSON, source, note string) (memorydomain.Fact, bool, error)
	ConfirmOnboarding(ctx context.Context, sessionID string, facts map[string]string) (string, error)
	HasComplianceProfile(ctx context.Context) (bool, error)
	ProfileFacts(ctx context.Context) ([]memorydomain.Fact, error)
}

// Service implements corev1connect.OnboardingServiceHandler.
//
// No role gate. Onboarding is the first thing a person does in an organisation
// they have just been provisioned into, and requiring an owner would strand
// anybody invited into a workspace whose owner has not got round to it. Which
// organisation they are answering about is RLS's decision, as everywhere.
type Service struct{}

func New() *Service { return &Service{} }

func (s *Service) GetOnboardingSession(
	ctx context.Context,
	_ *connect.Request[corev1.GetOnboardingSessionRequest],
) (*connect.Response[corev1.GetOnboardingSessionResponse], error) {
	tenant, err := tenantFrom(ctx)
	if err != nil {
		return nil, err
	}

	state, err := currentState(ctx, tenant)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&corev1.GetOnboardingSessionResponse{State: state}), nil
}

func (s *Service) StartOnboarding(
	ctx context.Context,
	_ *connect.Request[corev1.StartOnboardingRequest],
) (*connect.Response[corev1.StartOnboardingResponse], error) {
	tenant, err := tenantFrom(ctx)
	if err != nil {
		return nil, err
	}

	session, created, err := tenant.StartOnboardingSession(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	if created {
		if _, err := tenant.AppendOnboardingTurn(
			ctx, session.ID, domain.RoleAssistant, greeting, "", "",
		); err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
	}

	state, err := stateAfterAsking(ctx, tenant, session)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&corev1.StartOnboardingResponse{
		State:   state,
		Created: created,
	}), nil
}

func (s *Service) AnswerQuestion(
	ctx context.Context,
	request *connect.Request[corev1.AnswerQuestionRequest],
) (*connect.Response[corev1.AnswerQuestionResponse], error) {
	tenant, err := tenantFrom(ctx)
	if err != nil {
		return nil, err
	}

	key, ok := memoryservice.Keys[request.Msg.GetKey()]
	if !ok {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("that is not a question this product asks"))
	}
	if _, asked := domain.QuestionFor(key); !asked {
		// A fact the memory surface understands but the interview does not
		// collect. Refused rather than accepted, because an answer to a
		// question nobody asked has no prompt in the transcript to be read
		// against.
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("that fact is not part of the onboarding interview"))
	}

	session, err := tenant.OnboardingSession(ctx)
	if errors.Is(err, postgres.ErrNoOnboardingSession) {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("this organisation has not started onboarding yet"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if session.Status == domain.StatusCompleted {
		// Answers after the interview finished would sit in the transcript
		// changing nothing, because the profile is already written. Correcting
		// a fact is what changing an answer looks like from here, and it has
		// its own surface with its own history.
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("onboarding is finished for this organisation: correct a fact instead"))
	}

	content := request.Msg.GetAnswer()
	valueJSON := ""
	if request.Msg.GetSkip() {
		content = domain.SkippedContent
	} else {
		valueJSON, err = domain.Parse(key, content)
		if err != nil {
			// The caller's, and worth saying out loud: this is the refusal that
			// keeps the profile honest. "About fifty" does not become fifty
			// here and does not become fifty anywhere else either.
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
	}

	if _, err := tenant.AppendOnboardingTurn(
		ctx, session.ID, domain.RoleUser, content, key, valueJSON,
	); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// # THE ANSWER IS BELIEVED NOW, NOT AT A CONFIRMATION STEP (ENT-254)
	//
	// ENT-212 held answers in the transcript until `ConfirmProfile`, so that
	// nothing was believed the person had not read back. ENT-254's ruling is
	// that this flow should be easy, and a review-then-commit screen after
	// eleven questions is the opposite of that. What the step protected is
	// protected better here: the parsed value is shown beside the answer at the
	// moment it is given, which is where a mistake is cheapest to catch, and a
	// fact is correctable afterwards with its history intact.
	//
	// A skip writes nothing at all, which is why this is guarded on the value
	// rather than on the request: an absent fact is the record of a question
	// somebody declined, and a placeholder would be a guess wearing a value.
	if valueJSON != "" {
		if _, _, err := tenant.CorrectFact(ctx, key, valueJSON, "onboarding", ""); err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
	}

	session, err = finishIfDone(ctx, tenant, session)
	if err != nil {
		return nil, err
	}

	state, err := stateAfterAsking(ctx, tenant, session)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&corev1.AnswerQuestionResponse{State: state}), nil
}

// finishIfDone closes the interview once the last applicable question has an
// answer or a skip.
//
// # WHY FINISHING IS STILL A DISTINCT MOMENT WITH NO CONFIRMATION STEP
//
// The facts are already written by the time this runs. What is left is the
// `compliance_profiles` projection, the completed status and the sweep trigger,
// and all three want to happen exactly once, when there is a whole profile to
// reason from. Writing the projection on every answer would hand the Watcher a
// half-described organisation and produce findings from an interview nobody had
// finished, and `HasComplianceProfile` is what every authenticated route asks
// before routing somebody here, so it would also stop routing them back part
// way through.
//
// An interview where everything was skipped finishes nothing, deliberately. A
// profile of eleven defaults nobody stated is worse than no profile, because
// the Watcher cannot tell the difference.
func finishIfDone(
	ctx context.Context,
	tenant store,
	session domain.Session,
) (domain.Session, error) {
	if session.Status == domain.StatusCompleted {
		return session, nil
	}

	transcript, err := tenant.OnboardingTranscript(ctx, session.ID)
	if err != nil {
		return session, connect.NewError(connect.CodeInternal, err)
	}
	answers := domain.AnswersFrom(transcript)
	if _, more := domain.NextQuestion(answers); more {
		return session, nil
	}

	facts := domain.FactsFrom(answers)
	if len(facts) == 0 {
		return session, nil
	}
	if _, err := tenant.ConfirmOnboarding(ctx, session.ID, facts); err != nil {
		return session, connect.NewError(connect.CodeInternal, err)
	}
	session.Status = domain.StatusCompleted
	return session, nil
}

func (s *Service) ConfirmProfile(
	ctx context.Context,
	_ *connect.Request[corev1.ConfirmProfileRequest],
) (*connect.Response[corev1.ConfirmProfileResponse], error) {
	tenant, err := tenantFrom(ctx)
	if err != nil {
		return nil, err
	}

	session, err := tenant.OnboardingSession(ctx)
	if errors.Is(err, postgres.ErrNoOnboardingSession) {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("this organisation has not started onboarding yet"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	transcript, err := tenant.OnboardingTranscript(ctx, session.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	answers := domain.AnswersFrom(transcript)
	facts := domain.FactsFrom(answers)
	if len(facts) == 0 {
		// A profile of nothing would still be a profile: the Watcher would
		// start reasoning from a script's worth of defaults nobody stated and
		// produce findings the customer never gave it grounds for.
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("nothing has been answered yet, so there is nothing to record"))
	}

	profileID, err := tenant.ConfirmOnboarding(ctx, session.ID, facts)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	stored, err := tenant.ProfileFacts(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	response := &corev1.ConfirmProfileResponse{ProfileId: profileID}
	for _, fact := range stored {
		converted, err := memoryservice.ToProto(fact)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		response.Facts = append(response.Facts, converted)
	}

	response.State, err = currentState(ctx, tenant)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response), nil
}

// stateAfterAsking records the next question if it has not been asked, then
// reports the state.
//
// # ASKING IS A WRITE, AND IT HAS TO BE IDEMPOTENT
//
// The question a person is looking at is a row, so that a refresh, a second tab
// and a reconnect all show the same conversation. That means every call which
// advances the interview has to decide whether the next question is already in
// the transcript, and the answer has to be the same on a retry. It is: the
// question is appended only when the last turn is not already it.
func stateAfterAsking(
	ctx context.Context,
	tenant store,
	session domain.Session,
) (*corev1.OnboardingState, error) {
	transcript, err := tenant.OnboardingTranscript(ctx, session.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	answers := domain.AnswersFrom(transcript)
	question, more := domain.NextQuestion(answers)
	if more && !alreadyAsked(transcript, question.Key) {
		if _, err := tenant.AppendOnboardingTurn(
			ctx, session.ID, domain.RoleAssistant, question.Prompt, question.Key, "",
		); err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		transcript, err = tenant.OnboardingTranscript(ctx, session.ID)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
	}

	profileExists, err := tenant.HasComplianceProfile(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return buildState(session, transcript, profileExists), nil
}

// alreadyAsked reports whether the last turn is the question about this key.
//
// The LAST turn rather than any turn, because a person who goes back and
// re-answers an earlier question needs to be asked the next one again: the
// question was asked once, long ago, and the conversation has moved past it.
func alreadyAsked(transcript []domain.Turn, key string) bool {
	if len(transcript) == 0 {
		return false
	}
	last := transcript[len(transcript)-1]
	return last.Role == domain.RoleAssistant && last.Key == key
}

// currentState reports where the interview is, without writing anything.
func currentState(ctx context.Context, tenant store) (*corev1.OnboardingState, error) {
	profileExists, err := tenant.HasComplianceProfile(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	session, err := tenant.OnboardingSession(ctx)
	if errors.Is(err, postgres.ErrNoOnboardingSession) {
		// Never started. An empty state rather than an error: "there is nothing
		// here yet" is the answer every authenticated route is asking for, and
		// a NotFound would make every one of them handle a failure.
		return &corev1.OnboardingState{
			ProfileExists:  profileExists,
			TotalQuestions: int32(len(domain.Script())),
		}, nil
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	transcript, err := tenant.OnboardingTranscript(ctx, session.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return buildState(session, transcript, profileExists), nil
}

func buildState(
	session domain.Session,
	transcript []domain.Turn,
	profileExists bool,
) *corev1.OnboardingState {
	answers := domain.AnswersFrom(transcript)
	total, done := domain.Progress(answers)
	question, more := domain.NextQuestion(answers)

	state := &corev1.OnboardingState{
		SessionId:         session.ID,
		Status:            session.Status,
		ProfileExists:     profileExists,
		ReadyToConfirm:    !more,
		TotalQuestions:    int32(total),
		AnsweredQuestions: int32(done),
	}
	if more {
		state.NextQuestion = questionToProto(question)
	}

	for _, turn := range transcript {
		state.Transcript = append(state.Transcript, turnToProto(turn))
	}

	// Script order rather than map order, so the console reads the answers in
	// the order they were given every time this is rendered.
	for _, q := range domain.Script() {
		answer, given := answers[q.Key]
		if !given || answer.Skipped || answer.ValueJSON == "" {
			continue
		}
		value, err := memoryservice.JSONToValue(q.Key, answer.ValueJSON)
		if err != nil {
			// A stored answer this build cannot read back. Left out of the
			// draft rather than failing the whole call: the person can see the
			// rest and re-answer the one that is missing, where an error would
			// strand them with no way forward at all.
			continue
		}
		state.Draft = append(state.Draft, &corev1.DraftFact{
			Key:    keyToProto(q.Key),
			Value:  value,
			Answer: answer.Text,
		})
	}

	return state
}

func questionToProto(question domain.Question) *corev1.Question {
	out := &corev1.Question{
		Key:    keyToProto(question.Key),
		Prompt: question.Prompt,
		Help:   question.Help,
		Basis:  question.Basis,
		Shape:  shapeToProto(question.Shape()),
	}
	if question.Shape() == memorydomain.KindTriState {
		out.Choices = domain.TriStateChoices
	}
	// The closed vocabulary, sent so the console renders exactly the tokens the
	// server accepts. A console that invented one would produce an answer
	// `Parse` refuses, which is the safe direction for that disagreement to
	// fail in (ENT-254).
	for _, option := range question.Options {
		out.Options = append(out.Options, &corev1.QuestionOption{
			Value:     option.Value,
			Label:     option.Label,
			Exclusive: option.Exclusive,
		})
	}
	return out
}

func turnToProto(turn domain.Turn) *corev1.OnboardingTurn {
	out := &corev1.OnboardingTurn{
		Id:        turn.ID,
		Role:      turn.Role,
		Content:   turn.Content,
		Key:       keyToProto(turn.Key),
		Skipped:   turn.Skipped(),
		CreatedAt: turn.CreatedAt.Format(time.RFC3339Nano),
		CreatedBy: turn.CreatedBy,
	}
	if turn.ValueJSON != "" {
		if value, err := memoryservice.JSONToValue(turn.Key, turn.ValueJSON); err == nil {
			out.Value = value
		}
	}
	return out
}

func keyToProto(key string) corev1.ProfileFactKey {
	if wire, known := memoryservice.StoredKeys[key]; known {
		return wire
	}
	return corev1.ProfileFactKey_PROFILE_FACT_KEY_UNSPECIFIED
}

func shapeToProto(kind memorydomain.Kind) corev1.AnswerShape {
	switch kind {
	case memorydomain.KindText:
		return corev1.AnswerShape_ANSWER_SHAPE_TEXT
	case memorydomain.KindList:
		return corev1.AnswerShape_ANSWER_SHAPE_LIST
	case memorydomain.KindTriState:
		return corev1.AnswerShape_ANSWER_SHAPE_TRI_STATE
	case memorydomain.KindNumber:
		return corev1.AnswerShape_ANSWER_SHAPE_NUMBER
	}
	return corev1.AnswerShape_ANSWER_SHAPE_UNSPECIFIED
}

func tenantFrom(ctx context.Context) (store, error) {
	if _, ok := interceptor.ClaimsFrom(ctx); !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("handler reached with no verified identity"))
	}
	tenant, ok := interceptor.TenantFrom(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("handler reached with no tenant transaction"))
	}
	typed, ok := tenant.(store)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal,
			fmt.Errorf("the tenant transaction cannot reach onboarding"))
	}
	return typed, nil
}
