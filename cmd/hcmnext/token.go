// token.go implements the "hcmnext token" subcommand: a development
// convenience that mints a bearer credential with the exact same HMAC
// verifier "hcmnext serve" authenticates with (internal/trust.HMACVerifier),
// so a credential minted here is a credential that verifier accepts, with no
// second implementation of the token format to drift out of sync with it
// (internal/trust.HMACVerifier.Issue already exists for exactly this reason).
//
// It is a plain one-shot CLI action, not a bootstrap.Spec role: it opens no
// database, binds no listener and runs no lifecycle. It signs one JSON
// payload and prints the result.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Default claim values a minted credential carries when the caller does not
// override them. comp_admin/compensation_review are the role and purpose the
// Promotion workspace's compensation half is gated on
// (internal/trust/authz.RoleCompAdmin, authz.PurposeCompensationReview), so a
// token minted with no -roles/-purpose flags can read a full workspace page
// out of the box.
const (
	defaultTokenRoles   = "comp_admin"
	defaultTokenPurpose = "compensation_review"
	defaultTokenTTL     = 8 * time.Hour
)

// tokenParams is the parsed, validated -flag surface for "hcmnext token".
type tokenParams struct {
	hmacKey      string
	issuer       string
	audience     string
	tenant       string
	subject      string
	clientID     string
	kind         string
	roles        []string
	purpose      string
	orgScope     string
	ttl          time.Duration
	identityOnly bool
}

// parseTokenArgs declares and parses the token subcommand's flags. Errors are
// reported to stderr by the flag package itself (flag.ContinueOnError still
// prints usage on -h/parse failure); the caller only has to act on the
// returned error.
func parseTokenArgs(args []string, stderr io.Writer) (tokenParams, error) {
	fs := flag.NewFlagSet("token", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintf(stderr, "usage: hcmnext token [flags]\n\n"+
			"Mints a bearer credential with the same HMAC development verifier\n"+
			"\"hcmnext serve\" authenticates with, and prints it to stdout.\n\nflags:\n")
		fs.PrintDefaults()
	}

	// The flag's default is the environment variable, exactly like serve's
	// own -dev-hmac-key (see fieldDevHMACKey's Env: EnvDevHMACKey in
	// serveSpec): the same key can be handed to both commands the same way,
	// without ever appearing in a process listing.
	hmacKey := fs.String("dev-hmac-key", os.Getenv(EnvDevHMACKey), "development HMAC signing key, at least 32 bytes (env "+EnvDevHMACKey+")")
	issuer := fs.String("issuer", defaultIssuer, "the credential issuer to mint under; must match the target listener's -issuer")
	audience := fs.String("audience", defaultAudience, "the audience to mint for; must match the target listener's -audience")
	tenant := fs.String("tenant", "", "tenant slug the credential is issued for (required)")
	subject := fs.String("subject", "", "opaque subject identifier the credential authenticates (required)")
	clientID := fs.String("client-id", "", "stable machine client identity used for server-side quota admission")
	kind := fs.String("subject-kind", "human", "authenticated actor kind: human, agent, or integration")
	roles := fs.String("roles", defaultTokenRoles, "comma-separated role identifiers granted to the credential")
	purpose := fs.String("purpose", defaultTokenPurpose, "purpose of processing the credential is authorized for")
	orgScope := fs.String("org-scope", "", "organization scope the subject acts within; required to create intents (the kernel refuses an intent with no organization_scope_id)")
	ttl := fs.Duration("ttl", defaultTokenTTL, "how long the minted credential remains valid")
	identityOnly := fs.Bool("identity-only", false, "mint a machine identity credential with no caller-selected roles, scope, or purposes")

	if err := fs.Parse(args); err != nil {
		return tokenParams{}, err
	}

	var problems []string
	if len(*hmacKey) < minimumHMACKeyBytes {
		problems = append(problems, fmt.Sprintf("-dev-hmac-key must be at least %d bytes", minimumHMACKeyBytes))
	}
	if strings.TrimSpace(*tenant) == "" {
		problems = append(problems, "-tenant is required")
	}
	if strings.TrimSpace(*subject) == "" {
		problems = append(problems, "-subject is required")
	}
	if *ttl <= 0 {
		problems = append(problems, "-ttl must be positive")
	}
	if *kind != "human" && *kind != "agent" && *kind != "integration" {
		problems = append(problems, "-subject-kind must be human, agent, or integration")
	}
	if *kind != "human" && *ttl > 15*time.Minute {
		problems = append(problems, "machine credentials must live at most 15m; set -ttl")
	}
	if *kind != "human" {
		if *identityOnly {
			if strings.TrimSpace(*clientID) == "" {
				problems = append(problems, "identity-only machine credentials require -client-id")
			}
			fs.Visit(func(f *flag.Flag) {
				if f.Name == "roles" && strings.TrimSpace(f.Value.String()) != "" {
					problems = append(problems, "identity-only credentials cannot carry roles")
				}
				if f.Name == "purpose" && strings.TrimSpace(f.Value.String()) != "" {
					problems = append(problems, "identity-only credentials cannot carry purposes")
				}
			})
			if strings.TrimSpace(*orgScope) != "" {
				problems = append(problems, "identity-only credentials cannot carry organization scope")
			}
			*roles = ""
			*purpose = ""
		} else {
			fs.Visit(func(f *flag.Flag) {
				if f.Name == "roles" && strings.TrimSpace(f.Value.String()) != "" {
					problems = append(problems, "machine credentials cannot carry human roles")
				}
				if f.Name == "purpose" && f.Value.String() != "chat_integration" {
					problems = append(problems, "machine credentials require chat_integration purpose")
				}
			})
			*roles = ""
			*purpose = "chat_integration"
		}
	} else if *identityOnly {
		problems = append(problems, "-identity-only requires a machine subject kind")
	}
	if len(problems) > 0 {
		return tokenParams{}, fmt.Errorf("hcmnext token: %s", strings.Join(problems, "; "))
	}

	return tokenParams{
		hmacKey:      *hmacKey,
		issuer:       *issuer,
		audience:     *audience,
		tenant:       *tenant,
		subject:      *subject,
		clientID:     strings.TrimSpace(*clientID),
		kind:         *kind,
		roles:        splitAndTrim(*roles),
		purpose:      *purpose,
		orgScope:     strings.TrimSpace(*orgScope),
		ttl:          *ttl,
		identityOnly: *identityOnly,
	}, nil
}

// splitAndTrim splits a comma-separated flag value into its non-empty,
// trimmed elements.
func splitAndTrim(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// mintDevToken builds and signs the bearer token text for p, dating the
// credential's validity window from now. It is the whole of what "hcmnext
// token" does; everything else in this file is flag parsing and printing.
//
// Assurance and authentication method are fixed by the development issuer.
// Subject kind is explicit so a local agent can use its own short-lived
// identity rather than borrowing a person's credential.
func mintDevToken(p tokenParams, now time.Time) (string, error) {
	if p.kind == "" {
		p.kind = "human"
	}
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key:      []byte(p.hmacKey),
		Issuer:   p.issuer,
		Audience: p.audience,
	})
	if err != nil {
		return "", fmt.Errorf("hcmnext token: build the verifier: %w", err)
	}
	claims := trust.Claims{
		Issuer:               p.issuer,
		Audience:             p.audience,
		Subject:              p.subject,
		ClientID:             p.clientID,
		SubjectKind:          p.kind,
		Tenant:               p.tenant,
		OrganizationScopeID:  p.orgScope,
		Roles:                p.roles,
		Purposes:             optionalTokenPurpose(p.purpose),
		AuthenticationMethod: "bearer_token",
		Assurance:            "substantial",
		SessionRef:           "session-hcmnext-token-cli",
		IssuedAtUnix:         now.Add(-time.Minute).Unix(),
		ExpiresAtUnix:        now.Add(p.ttl).Unix(),
	}
	var token string
	if p.identityOnly {
		token, err = verifier.IssueIdentity(claims)
	} else {
		token, err = verifier.Issue(claims)
	}
	if err != nil {
		return "", fmt.Errorf("hcmnext token: mint the credential: %w", err)
	}
	return token, nil
}

func optionalTokenPurpose(value string) []string {
	if value == "" {
		return nil
	}
	return []string{value}
}

// runToken is the token subcommand's entry point: parse, mint, print. It
// returns the process exit code rather than calling os.Exit itself, so a test
// can drive it without ending the test process.
func runToken(args []string, stdout, stderr io.Writer, now func() time.Time) int {
	params, err := parseTokenArgs(args, stderr)
	if err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		fmt.Fprintln(stderr, err)
		return 1
	}
	token, err := mintDevToken(params, now().UTC())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(stdout, token)
	return 0
}
