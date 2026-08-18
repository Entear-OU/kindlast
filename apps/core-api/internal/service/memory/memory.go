// Package memory serves MemoryService: what Kindlast knows about an
// organisation (ENT-228, §26.5).
//
// # THIS IS WHAT MAKES THE MEMORY THE CUSTOMER'S
//
// §26.5 puts memory in Postgres under RLS rather than in an agent framework's
// store so a customer can see it, correct it, export it and have it erased. A
// schema alone does not deliver that. This service is the seeing and the
// correcting. Export is these same reads. Erasure is deleting the
// organisation, which cascades.
//
// # A VALUE IS TYPED ON THE WAY IN, TWICE
//
// The oneof stops a caller sending two arms at once; it does not stop them
// sending the wrong one, since `staff_count` as text is a perfectly valid
// protobuf message and jsonb would store it happily. So the key-to-kind
// pairing is checked in the domain package before anything is written, and the
// database enforces the invariants the pairing cannot: one open value per key,
// and closed values that nobody, including the migrator, can edit.
//
// # WHY CORRECTION IS HERE AND NOT ON IngestService
//
// ENT-228 routes profile patches "through IngestService". The typed-patch half
// is right and is what CorrectFact is. The routing half cannot be: IngestService
// sits behind `internal:ingest`, issued to service principals through client
// credentials and never to a browser, and a human correcting a fact about
// their own organisation holds a user token. Routing this through IngestService
// would mean either issuing internal scopes to people, which undoes what
// ENT-207 built, or writing without the person's identity, which loses
// `recorded_by` on the one write where "which human said so" matters most.
package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"

	domain "github.com/Entear-OU/kindlast/apps/core-api/internal/domain/memory"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	corev1 "github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1"
)

// store is what these handlers need of the request's transaction, declared
// where it is used (§21.6).
type store interface {
	ProfileFacts(ctx context.Context) ([]domain.Fact, error)
	FactHistory(ctx context.Context, key string) ([]domain.Fact, error)
	CorrectFact(ctx context.Context, key, valueJSON, source, note string) (domain.Fact, bool, error)
	Observations(ctx context.Context, pageSize int32, before time.Time) ([]domain.Observation, error)
}

// keys maps the wire enum onto the stored key.
//
// An explicit table rather than `strings.ToLower(enum.String())`, because a
// derived mapping ties the database's contents to the enum's spelling: rename
// the enum value for clarity and every stored row becomes unreadable, silently,
// with no migration to notice.
var keys = map[corev1.ProfileFactKey]string{
	corev1.ProfileFactKey_PROFILE_FACT_KEY_INDUSTRY:              domain.KeyIndustry,
	corev1.ProfileFactKey_PROFILE_FACT_KEY_EU_JURISDICTIONS:      domain.KeyEUJurisdictions,
	corev1.ProfileFactKey_PROFILE_FACT_KEY_DATA_CATEGORIES:       domain.KeyDataCategories,
	corev1.ProfileFactKey_PROFILE_FACT_KEY_DATA_SUBJECTS:         domain.KeyDataSubjects,
	corev1.ProfileFactKey_PROFILE_FACT_KEY_AI_SYSTEMS:            domain.KeyAISystems,
	corev1.ProfileFactKey_PROFILE_FACT_KEY_HAS_DPO:               domain.KeyHasDPO,
	corev1.ProfileFactKey_PROFILE_FACT_KEY_HAS_ROPA:              domain.KeyHasROPA,
	corev1.ProfileFactKey_PROFILE_FACT_KEY_TRANSFERS_OUTSIDE_EU:  domain.KeyTransfersOutsideEU,
	corev1.ProfileFactKey_PROFILE_FACT_KEY_TRANSFER_DESTINATIONS: domain.KeyTransferDestination,
	corev1.ProfileFactKey_PROFILE_FACT_KEY_STAFF_COUNT:           domain.KeyStaffCount,
}

var storedKeys = func() map[string]corev1.ProfileFactKey {
	out := make(map[string]corev1.ProfileFactKey, len(keys))
	for k, v := range keys {
		out[v] = k
	}
	return out
}()

// Service implements corev1connect.MemoryServiceHandler.
//
// No role gate on the reads. Every member sees what the product believes about
// their organisation, viewers included: a person who can be told a finding
// should be able to see the profile the finding was reasoned from.
type Service struct{}

func New() *Service { return &Service{} }

func (s *Service) ListProfileFacts(
	ctx context.Context,
	_ *connect.Request[corev1.ListProfileFactsRequest],
) (*connect.Response[corev1.ListProfileFactsResponse], error) {
	tenant, err := tenantFrom(ctx)
	if err != nil {
		return nil, err
	}

	facts, err := tenant.ProfileFacts(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	response := &corev1.ListProfileFactsResponse{}
	for _, fact := range facts {
		converted, err := toProto(fact)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		response.Facts = append(response.Facts, converted)
	}
	return connect.NewResponse(response), nil
}

func (s *Service) GetFactHistory(
	ctx context.Context,
	request *connect.Request[corev1.GetFactHistoryRequest],
) (*connect.Response[corev1.GetFactHistoryResponse], error) {
	tenant, err := tenantFrom(ctx)
	if err != nil {
		return nil, err
	}

	key, ok := keys[request.Msg.GetKey()]
	if !ok {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("that is not a fact this product understands"))
	}

	facts, err := tenant.FactHistory(ctx, key)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	response := &corev1.GetFactHistoryResponse{}
	for _, fact := range facts {
		converted, err := toProto(fact)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		response.Facts = append(response.Facts, converted)
	}
	return connect.NewResponse(response), nil
}

func (s *Service) CorrectFact(
	ctx context.Context,
	request *connect.Request[corev1.CorrectFactRequest],
) (*connect.Response[corev1.CorrectFactResponse], error) {
	tenant, err := tenantFrom(ctx)
	if err != nil {
		return nil, err
	}

	key, ok := keys[request.Msg.GetKey()]
	if !ok {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("that is not a fact this product understands"))
	}

	valueJSON, err := valueToJSON(request.Msg.GetValue())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	// `human`, and not from the request. This RPC is reached with a user
	// token, so the source is a fact about the call rather than something the
	// caller gets to assert: a console that could label its own writes
	// "integration" could make a guess look like an observation.
	fact, changed, err := tenant.CorrectFact(ctx, key, valueJSON, "human", request.Msg.GetNote())
	if err != nil {
		// A validation failure here is the caller's, not ours: the domain
		// package refuses a value whose shape does not match its key.
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	converted, err := toProto(fact)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&corev1.CorrectFactResponse{
		Fact:    converted,
		Changed: changed,
	}), nil
}

func (s *Service) ListEvidence(
	ctx context.Context,
	request *connect.Request[corev1.ListEvidenceRequest],
) (*connect.Response[corev1.ListEvidenceResponse], error) {
	tenant, err := tenantFrom(ctx)
	if err != nil {
		return nil, err
	}

	var before time.Time
	if token := request.Msg.GetPageToken(); token != "" {
		parsed, err := time.Parse(time.RFC3339Nano, token)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument,
				errors.New("that page token is not one we issued"))
		}
		before = parsed
	}

	pageSize := request.Msg.GetPageSize()
	observations, err := tenant.Observations(ctx, pageSize, before)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	response := &corev1.ListEvidenceResponse{}
	for _, o := range observations {
		response.Evidence = append(response.Evidence, &corev1.Evidence{
			Id:           o.ID,
			Source:       o.Source,
			Kind:         o.Kind,
			ConnectionId: o.ConnectionID,
			ObservedAt:   o.ObservedAt.Format(time.RFC3339Nano),
			FetchedAt:    o.FetchedAt.Format(time.RFC3339Nano),
			BodyJson:     o.BodyJSON,
			SupersededBy: o.SupersededBy,
		})
	}

	// A next token only when the page was full. Issuing one on a short page
	// would send a console round again for nothing, and "there might be more"
	// is a worse answer than "there is not".
	if len(observations) > 0 && int32(len(observations)) == effectivePageSize(pageSize) {
		last := observations[len(observations)-1]
		response.NextPageToken = last.ObservedAt.Format(time.RFC3339Nano)
	}
	return connect.NewResponse(response), nil
}

// effectivePageSize mirrors the store's clamp, so the "was the page full"
// question is asked against the size that was actually used.
func effectivePageSize(requested int32) int32 {
	if requested <= 0 || requested > 200 {
		return 50
	}
	return requested
}

// valueToJSON turns the typed oneof into the jsonb the column holds.
//
// The oneof arm decides the JSON shape, so a caller cannot smuggle a list into
// a text fact by sending clever JSON: there is no place on this surface to send
// JSON at all.
func valueToJSON(value *corev1.FactValue) (string, error) {
	if value == nil {
		return "", errors.New("a fact needs a value")
	}

	var payload any
	switch arm := value.GetValue().(type) {
	case *corev1.FactValue_Text:
		payload = arm.Text
	case *corev1.FactValue_Number:
		payload = arm.Number
	case *corev1.FactValue_List:
		// An empty list rather than null when the arm is set with no items:
		// "we operate no AI systems" is an answer, and null would read as "we
		// did not ask".
		items := arm.List.GetValues()
		if items == nil {
			items = []string{}
		}
		payload = items
	case *corev1.FactValue_TriState:
		switch arm.TriState {
		case corev1.TriState_TRI_STATE_YES:
			payload = "yes"
		case corev1.TriState_TRI_STATE_NO:
			payload = "no"
		case corev1.TriState_TRI_STATE_UNSURE:
			payload = "unsure"
		default:
			return "", errors.New("yes, no or unsure")
		}
	default:
		return "", errors.New("a fact needs a value")
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encoding the value: %w", err)
	}
	return string(encoded), nil
}

func toProto(fact domain.Fact) (*corev1.ProfileFact, error) {
	key, known := storedKeys[fact.Key]
	if !known {
		// A stored key with no enum arm. Reachable only if the vocabulary
		// shrank while rows survived, which is a deployment mistake rather
		// than a request problem, and returning it as UNSPECIFIED would show a
		// customer a blank fact rather than telling anybody.
		return nil, fmt.Errorf("stored fact %q has no wire representation", fact.Key)
	}

	value, err := jsonToValue(fact.Key, fact.ValueJSON)
	if err != nil {
		return nil, err
	}

	out := &corev1.ProfileFact{
		Key:        key,
		Value:      value,
		Source:     fact.Source,
		EvidenceId: fact.EvidenceID,
		ValidFrom:  fact.ValidFrom.Format(time.RFC3339Nano),
		RecordedBy: fact.RecordedBy,
	}
	if fact.ValidTo != nil {
		out.ValidTo = fact.ValidTo.Format(time.RFC3339Nano)
	}
	return out, nil
}

func jsonToValue(key, valueJSON string) (*corev1.FactValue, error) {
	kind, known := domain.Kinds[key]
	if !known {
		return nil, fmt.Errorf("stored fact %q has no kind", key)
	}

	var decoded any
	if err := json.Unmarshal([]byte(valueJSON), &decoded); err != nil {
		return nil, fmt.Errorf("decoding the stored value of %q: %w", key, err)
	}

	switch kind {
	case domain.KindText:
		text, ok := decoded.(string)
		if !ok {
			return nil, fmt.Errorf("stored value of %q is not text", key)
		}
		return &corev1.FactValue{Value: &corev1.FactValue_Text{Text: text}}, nil
	case domain.KindNumber:
		number, ok := decoded.(float64)
		if !ok {
			return nil, fmt.Errorf("stored value of %q is not a number", key)
		}
		return &corev1.FactValue{Value: &corev1.FactValue_Number{Number: int64(number)}}, nil
	case domain.KindTriState:
		text, _ := decoded.(string)
		var state corev1.TriState
		switch text {
		case "yes":
			state = corev1.TriState_TRI_STATE_YES
		case "no":
			state = corev1.TriState_TRI_STATE_NO
		case "unsure":
			state = corev1.TriState_TRI_STATE_UNSURE
		default:
			return nil, fmt.Errorf("stored value of %q is not yes, no or unsure", key)
		}
		return &corev1.FactValue{Value: &corev1.FactValue_TriState{TriState: state}}, nil
	case domain.KindList:
		items, ok := decoded.([]any)
		if !ok {
			return nil, fmt.Errorf("stored value of %q is not a list", key)
		}
		values := make([]string, 0, len(items))
		for _, item := range items {
			text, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("stored value of %q holds a non-text item", key)
			}
			values = append(values, text)
		}
		return &corev1.FactValue{
			Value: &corev1.FactValue_List{List: &corev1.StringList{Values: values}},
		}, nil
	}
	return nil, fmt.Errorf("stored fact %q has an unknown kind", key)
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
			errors.New("the tenant transaction cannot reach organisation memory"))
	}
	return typed, nil
}
