package schedule

import (
	"errors"
	"testing"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
)

// The Messenger between the plan and the send (ENT-280): what the workflow
// does with the drafting instruction, and what it must never do, which is let
// any outcome of drafting stop the doorbell.

func plannedWithDraft(users ...string) *platformv1.PlanNotificationResponse {
	res := &platformv1.PlanNotificationResponse{
		Draft: &platformv1.DraftInstruction{
			OrgId: "org-1",
			Context: &platformv1.MessageContext{
				OrgName:  "Acme Ltd",
				Severity: "high",
			},
		},
	}
	for _, u := range users {
		res.Recipients = append(res.Recipients,
			planned(u, platformv1.PlannedRecipient_DECISION_SEND, time.Time{}, ""))
	}
	return res
}

func registerDraft(env *testsuite.TestWorkflowEnvironment,
	fn func(*platformv1.DraftMessageRequest) (*platformv1.DraftMessageResponse, error),
) {
	env.RegisterActivityWithOptions(fn,
		activity.RegisterOptions{Name: DraftMessageActivityName})
}

func TestDraftedWordsRideToTheSend(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	mail := &fakeDeliverer{plans: []*platformv1.PlanNotificationResponse{plannedWithDraft("ada")}}
	registerMail(env, &Activities{Mail: mail})
	var asked *platformv1.DraftMessageRequest
	registerDraft(env, func(req *platformv1.DraftMessageRequest) (*platformv1.DraftMessageResponse, error) {
		asked = req
		return &platformv1.DraftMessageResponse{
			Outcome:  platformv1.MessageOutcome_MESSAGE_OUTCOME_SUCCEEDED,
			Subject:  "A serious finding is waiting on you in Acme Ltd",
			BodyText: "Something in Acme Ltd's compliance record needs a decision.",
		}, nil
	})

	env.ExecuteWorkflow(DeliverNotificationWorkflow, "n1")

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("the notification failed: %v", err)
	}
	if asked == nil {
		t.Fatal("the Messenger was never asked despite the plan carrying an instruction")
	}
	if asked.GetNotificationId() != "n1" || asked.GetOrgId() != "org-1" {
		t.Fatalf("the ask names %q/%q, want n1/org-1", asked.GetNotificationId(), asked.GetOrgId())
	}
	if len(mail.notifiedWith) != 1 {
		t.Fatalf("%d sends, want 1", len(mail.notifiedWith))
	}
	sent := mail.notifiedWith[0]
	if sent.GetSubject() != "A serious finding is waiting on you in Acme Ltd" ||
		sent.GetBodyText() == "" {
		t.Fatalf("the drafted words did not ride to the send: %+v", sent)
	}
}

// THE PROPERTY THE WHOLE WIRING RESTS ON. Every way the draft step can end
// other than a drafted message reads as "use the template", and the doorbell
// rings either way. A model that could stop a notification by failing would
// hold a customer's awareness of their own finding hostage to a GPU.
func TestTheDoorbellStillRingsWhenTheDraftFails(t *testing.T) {
	cases := map[string]func(*platformv1.DraftMessageRequest) (*platformv1.DraftMessageResponse, error){
		"the activity errors": func(*platformv1.DraftMessageRequest) (*platformv1.DraftMessageResponse, error) {
			return nil, errors.New("the model host is down")
		},
		"the run is refused": func(*platformv1.DraftMessageRequest) (*platformv1.DraftMessageResponse, error) {
			return &platformv1.DraftMessageResponse{
				Outcome:       platformv1.MessageOutcome_MESSAGE_OUTCOME_REFUSED,
				OutcomeDetail: "the draft contained a link",
			}, nil
		},
		"the run failed": func(*platformv1.DraftMessageRequest) (*platformv1.DraftMessageResponse, error) {
			return &platformv1.DraftMessageResponse{
				Outcome:       platformv1.MessageOutcome_MESSAGE_OUTCOME_FAILED,
				OutcomeDetail: "the model timed out",
			}, nil
		},
	}
	for name, stub := range cases {
		t.Run(name, func(t *testing.T) {
			var suite testsuite.WorkflowTestSuite
			env := suite.NewTestWorkflowEnvironment()
			mail := &fakeDeliverer{plans: []*platformv1.PlanNotificationResponse{plannedWithDraft("ada")}}
			registerMail(env, &Activities{Mail: mail})
			registerDraft(env, stub)

			env.ExecuteWorkflow(DeliverNotificationWorkflow, "n1")

			if err := env.GetWorkflowError(); err != nil {
				t.Fatalf("the doorbell did not ring: %v", err)
			}
			if len(mail.notifiedWith) != 1 {
				t.Fatalf("%d sends, want 1", len(mail.notifiedWith))
			}
			if s := mail.notifiedWith[0]; s.GetSubject() != "" || s.GetBodyText() != "" {
				t.Fatalf("a failed draft still handed words to the send: %+v", s)
			}
			if mail.settledWith.GetOutcome() != platformv1.SettleNotificationRequest_OUTCOME_SENT {
				t.Fatalf("settled %v, want sent", mail.settledWith.GetOutcome())
			}
		})
	}
}
