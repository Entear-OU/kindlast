package modelchoice_test

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/modelchoice"
)

func resolving(addrs ...string) modelchoice.Lookup {
	return func(_ context.Context, _ string) ([]netip.Addr, error) {
		out := make([]netip.Addr, 0, len(addrs))
		for _, a := range addrs {
			out = append(out, netip.MustParseAddr(a))
		}
		return out, nil
	}
}

func TestParseProvidersReadsTheOperatorsList(t *testing.T) {
	providers, err := modelchoice.ParseProviders(
		"openai=api.openai.com, azure=.openai.azure.com")
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	if len(providers) != 2 {
		t.Fatalf("got %d providers, want 2", len(providers))
	}
	if providers[0].Name != "openai" || providers[0].Host != "api.openai.com" {
		t.Fatalf("first provider is %+v", providers[0])
	}
	if providers[1].Host != ".openai.azure.com" {
		t.Fatalf("second provider is %+v", providers[1])
	}
}

// AN EMPTY SETTING IS THE DEFAULT AND IT MEANS NOBODY MAY DO THIS.
//
// The product's claim is that a deployment holding a compliance record can run
// with no outbound internet at all. A setting that defaulted to permitting a
// provider would make that claim depend on an operator remembering to switch
// something off.
func TestNoProvidersConfiguredPermitsNothing(t *testing.T) {
	providers, err := modelchoice.ParseProviders("")
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	if len(providers) != 0 {
		t.Fatalf("an empty setting produced %d providers", len(providers))
	}
	if _, err := modelchoice.Permitted(providers, "openai"); !errors.Is(err, modelchoice.ErrNotPermitted) {
		t.Fatalf("an unconfigured deployment permitted openai: %v", err)
	}
}

func TestParseProvidersRefusesEntriesThatCannotBeChecked(t *testing.T) {
	for _, spec := range []string{
		"openai",          // no host, so nothing bounds the endpoint
		"=api.openai.com", // no name, so no audit row can name it
		"openai=",         // empty host
		"openai=api.openai.com,openai=evil.example.com", // one name, two hosts
		"openai=https://api.openai.com",                 // a URL, not a host
	} {
		if _, err := modelchoice.ParseProviders(spec); err == nil {
			t.Fatalf("%q was accepted", spec)
		}
	}
}

func TestPermittedIsTheOnlyWayAProviderExists(t *testing.T) {
	providers, err := modelchoice.ParseProviders("openai=api.openai.com")
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	if _, err := modelchoice.Permitted(providers, "openai"); err != nil {
		t.Fatalf("openai was refused: %v", err)
	}
	if _, err := modelchoice.Permitted(providers, "anthropic"); !errors.Is(err, modelchoice.ErrNotPermitted) {
		t.Fatalf("anthropic was permitted: %v", err)
	}
}

func TestValidateEndpointAcceptsAPermittedPublicHTTPSEndpoint(t *testing.T) {
	provider := modelchoice.Provider{Name: "openai", Host: "api.openai.com"}
	err := modelchoice.ValidateEndpoint(context.Background(),
		"https://api.openai.com", provider, resolving("104.18.7.192"))
	if err != nil {
		t.Fatalf("a legitimate endpoint was refused: %v", err)
	}
}

func TestValidateEndpointRefusesTheInsideOfTheDeployment(t *testing.T) {
	// EVERY ONE OF THESE IS AN SSRF, and the check is on the RESOLVED address
	// rather than on the string, because a hostname an attacker controls can
	// point anywhere they like and reads as perfectly ordinary.
	provider := modelchoice.Provider{Name: "byo", Host: "models.example.com"}
	for name, addr := range map[string]string{
		"loopback":        "127.0.0.1",
		"loopback v6":     "::1",
		"rfc1918":         "10.1.2.3",
		"rfc1918 172":     "172.16.9.9",
		"rfc1918 192":     "192.168.0.7",
		"link local":      "169.254.169.254",
		"unique local v6": "fd00::1",
		"link local v6":   "fe80::1",
		"cgnat":           "100.64.0.1",
		"unspecified":     "0.0.0.0",
		"multicast":       "239.1.1.1",
		"mapped rfc1918":  "::ffff:10.0.0.1",
	} {
		err := modelchoice.ValidateEndpoint(context.Background(),
			"https://models.example.com", provider, resolving(addr))
		if !errors.Is(err, modelchoice.ErrPrivateAddress) {
			t.Errorf("%s (%s) was accepted: %v", name, addr, err)
		}
	}
}

// ONE PUBLIC ANSWER DOES NOT EXCUSE A PRIVATE ONE. A host that resolves to
// both is the shape of the attack, not an edge case.
func TestValidateEndpointRefusesAHostThatAlsoResolvesInside(t *testing.T) {
	provider := modelchoice.Provider{Name: "byo", Host: "models.example.com"}
	err := modelchoice.ValidateEndpoint(context.Background(),
		"https://models.example.com", provider,
		resolving("104.18.7.192", "169.254.169.254"))
	if !errors.Is(err, modelchoice.ErrPrivateAddress) {
		t.Fatalf("a split answer was accepted: %v", err)
	}
}

func TestValidateEndpointRefusesWhatCannotBeTrusted(t *testing.T) {
	provider := modelchoice.Provider{Name: "openai", Host: "api.openai.com"}
	cases := map[string]string{
		"plain http":             "http://api.openai.com",
		"another scheme":         "file:///etc/passwd",
		"another host":           "https://evil.example.com",
		"host as a prefix":       "https://api.openai.com.evil.example.com",
		"credentials in the url": "https://sk-secret@api.openai.com",
		"no host at all":         "https://",
		"not a url":              "::::",
	}
	for name, raw := range cases {
		if err := modelchoice.ValidateEndpoint(context.Background(), raw, provider,
			resolving("104.18.7.192")); err == nil {
			t.Errorf("%s (%q) was accepted", name, raw)
		}
	}
}

// A SUFFIX ENTRY MATCHES A SUBDOMAIN AND NOT THE PARENT, because `azure` in
// the operator's list means "our own Azure resources", and a bare
// `openai.azure.com` is not one of them.
func TestSuffixProvidersMatchSubdomainsOnly(t *testing.T) {
	provider := modelchoice.Provider{Name: "azure", Host: ".openai.azure.com"}
	if err := modelchoice.ValidateEndpoint(context.Background(),
		"https://acme.openai.azure.com", provider, resolving("20.1.2.3")); err != nil {
		t.Fatalf("a subdomain was refused: %v", err)
	}
	if err := modelchoice.ValidateEndpoint(context.Background(),
		"https://openai.azure.com", provider, resolving("20.1.2.3")); err == nil {
		t.Fatal("the bare parent domain was accepted")
	}
	if err := modelchoice.ValidateEndpoint(context.Background(),
		"https://notopenai.azure.com", provider, resolving("20.1.2.3")); err == nil {
		t.Fatal("a host merely ending in the suffix was accepted")
	}
}

func TestLastFourNeverLeaksMoreThanFour(t *testing.T) {
	if got := modelchoice.LastFour("sk-proj-abcdefgh1234"); got != "1234" {
		t.Fatalf("last four is %q", got)
	}
	// Too short to have a safe tail, so it has none. Showing "ab" of a
	// two-character key would be showing the key.
	if got := modelchoice.LastFour("ab"); got != "" {
		t.Fatalf("a short key produced %q", got)
	}
	// Non-alphanumeric tails are dropped rather than rendered, because the
	// column's check constraint refuses them and a store error at the end of a
	// write is a worse answer than no hint.
	if got := modelchoice.LastFour("secret--"); got != "" {
		t.Fatalf("a punctuation tail produced %q", got)
	}
}

// THE CONSEQUENCE IS ONE SENTENCE IN ONE PLACE. A console writing its own
// version is a console whose warning drifts from what the product does, and a
// self-hoster's alternative console would have none at all.
func TestConsequenceNoticeSaysWhatActuallyHappens(t *testing.T) {
	notice := modelchoice.ConsequenceNotice
	if strings.TrimSpace(notice) == "" {
		t.Fatal("there is no consequence notice")
	}
	for _, word := range []string{"sub-processor", "leave"} {
		if !strings.Contains(notice, word) {
			t.Errorf("the notice does not mention %q: %s", word, notice)
		}
	}
	if strings.ContainsAny(notice, "–—") {
		t.Error("the notice contains a dash this repository does not use")
	}
}
