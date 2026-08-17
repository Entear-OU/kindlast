package org

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/notify"
	domain "github.com/Entear-OU/kindlast/apps/core-api/internal/domain/org"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
	corev1 "github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1"
)

// managing is what these handlers need of the request's transaction, declared
// where it is used rather than exported from the store (§21.6).
type managing interface {
	Members(ctx context.Context) ([]domain.Member, error)
	CreateOrganisation(ctx context.Context, name string) (domain.Joined, error)
	RenameOrganisation(ctx context.Context, name string) (domain.Joined, error)
	SetMemberRole(ctx context.Context, userID, role string) error
	RemoveMember(ctx context.Context, userID string) error
	CreateInvitation(ctx context.Context, email, role, token string) (domain.Invitation, error)
	// Both needed by InviteMember, and both on this interface rather than a
	// second one because they run in the same transaction as CreateInvitation:
	// the invitation and the message that carries it commit together or not at
	// all (ENT-219).
	OrganisationName(ctx context.Context) (string, error)
	EnqueueMessage(ctx context.Context, msg notify.Message) error
	Role() string
	UserID() string
}

// tenantFor is the preamble every handler here shares: a verified identity and
// a transaction that can do organisation work.
//
// Each failure is Internal rather than Unauthenticated on purpose. Reaching a
// handler without claims or without a tenant means an interceptor did not run,
// which is a wiring fault in this process, not something the caller did.
func tenantFor(ctx context.Context) (managing, error) {
	if _, ok := interceptor.ClaimsFrom(ctx); !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("handler reached with no verified identity"))
	}
	tenant, ok := interceptor.TenantFrom(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("handler reached with no tenant transaction"))
	}
	store, ok := tenant.(managing)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("the tenant transaction cannot manage organisations"))
	}
	return store, nil
}

// requireOwner is the role boundary, and it is the second of three checks
// rather than the only one.
//
// The scope interceptor has already refused a token without `org:manage`, and
// RLS will refuse the write regardless of what happens here. This exists so
// the caller gets an answer that says what was wrong instead of a successful
// call that changed nothing, which is what an RLS refusal looks like from
// outside.
func requireOwner(store managing) error {
	if store.Role() != domain.RoleOwner {
		return connect.NewError(connect.CodePermissionDenied,
			errors.New("only an owner can do this"))
	}
	return nil
}

func trimmedName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", connect.NewError(connect.CodeInvalidArgument,
			errors.New("an organisation needs a name"))
	}
	return name, nil
}

// CreateOrganisation creates an organisation owned by the caller.
//
// No owner check: there is no organisation to be an owner of yet. This is one
// of the two calls that carry no active organisation.
func (s *Service) CreateOrganisation(
	ctx context.Context,
	req *connect.Request[corev1.CreateOrganisationRequest],
) (*connect.Response[corev1.CreateOrganisationResponse], error) {
	store, err := tenantFor(ctx)
	if err != nil {
		return nil, err
	}
	name, err := trimmedName(req.Msg.GetName())
	if err != nil {
		return nil, err
	}

	created, err := store.CreateOrganisation(ctx, name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&corev1.CreateOrganisationResponse{
		OrgId: created.OrgID,
		Name:  created.OrgName,
		Slug:  created.OrgSlug,
		Role:  created.Role,
	}), nil
}

// UpdateOrganisation renames the active organisation, leaving its slug alone.
func (s *Service) UpdateOrganisation(
	ctx context.Context,
	req *connect.Request[corev1.UpdateOrganisationRequest],
) (*connect.Response[corev1.UpdateOrganisationResponse], error) {
	store, err := tenantFor(ctx)
	if err != nil {
		return nil, err
	}
	if err := requireOwner(store); err != nil {
		return nil, err
	}
	name, err := trimmedName(req.Msg.GetName())
	if err != nil {
		return nil, err
	}

	renamed, err := store.RenameOrganisation(ctx, name)
	if errors.Is(err, postgres.ErrNoSuchMember) {
		return nil, connect.NewError(connect.CodePermissionDenied,
			errors.New("only an owner can rename this organisation"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&corev1.UpdateOrganisationResponse{
		OrgId: renamed.OrgID,
		Name:  renamed.OrgName,
		Slug:  renamed.OrgSlug,
	}), nil
}

// ListMembers returns everyone in the active organisation.
func (s *Service) ListMembers(
	ctx context.Context,
	_ *connect.Request[corev1.ListMembersRequest],
) (*connect.Response[corev1.ListMembersResponse], error) {
	store, err := tenantFor(ctx)
	if err != nil {
		return nil, err
	}

	members, err := store.Members(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	out := make([]*corev1.Member, 0, len(members))
	for _, m := range members {
		out = append(out, toProtoMember(m))
	}
	return connect.NewResponse(&corev1.ListMembersResponse{Members: out}), nil
}

// UpdateMemberRole changes a member's role, refusing to strip the last owner.
func (s *Service) UpdateMemberRole(
	ctx context.Context,
	req *connect.Request[corev1.UpdateMemberRoleRequest],
) (*connect.Response[corev1.UpdateMemberRoleResponse], error) {
	store, err := tenantFor(ctx)
	if err != nil {
		return nil, err
	}
	if err := requireOwner(store); err != nil {
		return nil, err
	}

	role := req.Msg.GetRole()
	if !domain.ValidRole(role) {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("role must be one of %s, %s or %s",
				domain.RoleOwner, domain.RoleMember, domain.RoleViewer))
	}

	userID := req.Msg.GetUserId()
	if err := s.refuseIfLastOwner(ctx, store, userID, role); err != nil {
		return nil, err
	}

	if err := store.SetMemberRole(ctx, userID, role); errors.Is(err, postgres.ErrNoSuchMember) {
		return nil, connect.NewError(connect.CodeNotFound,
			errors.New("no such member in this organisation"))
	} else if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// Re-read rather than construct the answer, so the response describes what
	// the database holds instead of what this handler believes it wrote.
	members, err := store.Members(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	for _, m := range members {
		if m.UserID == userID {
			return connect.NewResponse(&corev1.UpdateMemberRoleResponse{
				Member: toProtoMember(m),
			}), nil
		}
	}
	return nil, connect.NewError(connect.CodeInternal,
		errors.New("the member vanished between the write and the read"))
}

// RemoveMember removes someone, refusing to remove the last owner.
func (s *Service) RemoveMember(
	ctx context.Context,
	req *connect.Request[corev1.RemoveMemberRequest],
) (*connect.Response[corev1.RemoveMemberResponse], error) {
	store, err := tenantFor(ctx)
	if err != nil {
		return nil, err
	}
	if err := requireOwner(store); err != nil {
		return nil, err
	}

	userID := req.Msg.GetUserId()
	if err := s.refuseIfLastOwner(ctx, store, userID, ""); err != nil {
		return nil, err
	}

	if err := store.RemoveMember(ctx, userID); errors.Is(err, postgres.ErrNoSuchMember) {
		return nil, connect.NewError(connect.CodeNotFound,
			errors.New("no such member in this organisation"))
	} else if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&corev1.RemoveMemberResponse{}), nil
}

// refuseIfLastOwner reads the membership list and applies the domain rule.
//
// Read inside the same transaction as the write that follows, which is what
// makes the check meaningful: the list cannot change underneath it, so two
// concurrent owners cannot each be told they are safe to leave.
func (s *Service) refuseIfLastOwner(
	ctx context.Context, store managing, userID, newRole string,
) error {
	members, err := store.Members(ctx)
	if err != nil {
		return connect.NewError(connect.CodeInternal, err)
	}
	if domain.WouldLeaveNoOwner(members, userID, newRole) {
		return connect.NewError(connect.CodeFailedPrecondition,
			errors.New("an organisation must keep at least one owner"))
	}
	return nil
}

// InviteMember records an invitation and queues the email carrying it.
//
// The raw token is never returned, because returning it would let anyone who
// can invite also redeem on the invitee's behalf. It is rendered into a message
// and written to the outbox in the same transaction as the invitation, which is
// the whole of ENT-219: the token exists for the life of this call and nothing
// can reconstruct it afterwards, so an invitation committed without its message
// is permanently undeliverable rather than merely unsent.
func (s *Service) InviteMember(
	ctx context.Context,
	req *connect.Request[corev1.InviteMemberRequest],
) (*connect.Response[corev1.InviteMemberResponse], error) {
	// Checked before anything is minted. An invitation whose link cannot be
	// built must not exist: it would be stored, counted, and shown in the
	// members list as pending, while being impossible to accept and impossible
	// to repair, because reissuing produces a different token and the original
	// row still sits there looking valid.
	//
	// Refusing is therefore the honest answer, and `failed_precondition` is the
	// honest code: the caller is entitled to invite, the deployment is not
	// configured to carry one yet. Defaulting to a guessed localhost would be
	// worse than refusing, because it produces links that resolve for whoever
	// happens to be running the console on that machine and for nobody else.
	if !notify.ValidBaseURL(s.appBaseURL) {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("this deployment cannot send invitations yet: "+
				"KINDLAST_APP_BASE_URL is not set to the address the console is served from"))
	}

	store, err := tenantFor(ctx)
	if err != nil {
		return nil, err
	}
	if err := requireOwner(store); err != nil {
		return nil, err
	}

	email := strings.TrimSpace(req.Msg.GetEmail())
	if email == "" || !strings.Contains(email, "@") {
		// Deliberately shallow. Anything stricter rejects addresses that are
		// valid under RFC 5321, and the authoritative test of an address is
		// whether mail to it arrives.
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("an invitation needs an email address"))
	}
	role := req.Msg.GetRole()
	if !domain.ValidRole(role) {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("role must be one of %s, %s or %s",
				domain.RoleOwner, domain.RoleMember, domain.RoleViewer))
	}

	token, err := newInvitationToken()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// Read before the invitation is written so the message can name the
	// organisation. Inside the same transaction, so it is the name as it is at
	// mint rather than whatever it becomes before delivery.
	orgName, err := store.OrganisationName(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	invitation, err := store.CreateInvitation(ctx, email, role, token)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// The mint-time egress (doc §20.1). This is the only moment the raw token
	// is in memory, so it is the only moment a redeemable link can be written.
	// The interceptor commits the transaction, so a failure here rolls the
	// invitation back too, which is the intended pairing: no invitation without
	// a way to accept it.
	message := notify.Invitation(
		email,
		orgName,
		notify.InvitationLink(s.appBaseURL, token),
		int(postgres.InvitationLifetime.Hours()/24),
	)
	if err := store.EnqueueMessage(ctx, message); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&corev1.InviteMemberResponse{
		InvitationId: invitation.ID,
		ExpiresAt:    timestamppb.New(invitation.ExpiresAt),
	}), nil
}

// newInvitationToken mints a bearer capability.
//
// 32 bytes from crypto/rand, base64url without padding so it survives a URL
// path segment untouched. Not a uuid: a uuid is an identifier that happens to
// be hard to guess, and this is a secret whose whole job is being unguessable.
func newInvitationToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generating an invitation token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func toProtoMember(m domain.Member) *corev1.Member {
	return &corev1.Member{
		UserId:      m.UserID,
		Role:        m.Role,
		DisplayName: m.DisplayName,
		Email:       m.Email,
		JoinedAt:    timestamppb.New(m.JoinedAt),
	}
}
