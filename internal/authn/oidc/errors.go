package oidc

import "errors"

// Sentinel errors this package returns. All are matchable with errors.Is;
// wrapping context (which issuer, which claim, which endpoint) is added with
// fmt.Errorf("%w: ...", <sentinel>, ...) at the call site rather than by
// minting a new error value, so a caller can always test for the class of
// refusal without string matching.
var (
	// ErrInvalidFlowConfig is returned by [NewFlow] when a required
	// dependency or the configured secret is missing or malformed.
	ErrInvalidFlowConfig = errors.New("oidc: invalid flow configuration")

	// ErrInvalidClientRegistration is returned when a [ClientRegistration]
	// is missing a required field or an endpoint/redirect URI does not
	// parse as an absolute URL.
	ErrInvalidClientRegistration = errors.New("oidc: invalid client registration")

	// ErrClientNotRegistered is returned when no [ClientRegistration]
	// exists for the (tenant, issuer) pair a caller is beginning
	// authorization for, or when the registration on file is bound to a
	// different tenant or issuer than the one requested.
	ErrClientNotRegistered = errors.New("oidc: no client registration for this tenant and issuer")

	// ErrAuthorizationDenied is returned when the callback itself carries
	// an OAuth "error" parameter (the identity provider declined the
	// authorization request, e.g. the user denied consent).
	ErrAuthorizationDenied = errors.New("oidc: authorization server reported an error on the callback")

	// ErrCallbackMalformed is returned when a callback is missing its
	// state or code parameter.
	ErrCallbackMalformed = errors.New("oidc: callback is missing state or code")

	// ErrReplayedOrUnknownState is returned when the callback's state does
	// not name a pending authorization this flow still holds -- because it
	// was never issued, because it was already consumed by an earlier
	// callback (a replay of the same state and code, which always travel
	// together in one redirect), or because a concurrent callback consumed
	// it first.
	ErrReplayedOrUnknownState = errors.New("oidc: state is unknown or has already been used")

	// ErrVerifierExpired is returned when a pending authorization is found
	// but its TTL has elapsed before the callback arrived.
	ErrVerifierExpired = errors.New("oidc: pending authorization exceeded its TTL")

	// ErrPKCEDerivationMismatch is an internal consistency guard: it fires
	// only if the code_challenge recomputed from the deterministically
	// re-derived verifier does not equal the digest [Flow.BeginAuthorization]
	// stored, which should never happen unless the flow's secret changed
	// between the two calls (for example a misconfigured multi-replica
	// deployment where [FlowConfig.Secret] was not held constant).
	ErrPKCEDerivationMismatch = errors.New("oidc: derived PKCE verifier does not match the stored challenge")

	// ErrGrantRejected is returned when the token endpoint's response
	// carries a non-empty OAuth "error" field. RFC 6749 deliberately
	// collapses replayed/expired codes, PKCE verification failures,
	// redirect_uri mismatches and client authentication failures into the
	// single "invalid_grant" code so a caller cannot distinguish them from
	// the wire response alone; this sentinel is therefore intentionally
	// coarse; the wrapped error text carries whatever error/error_description
	// the identity provider returned.
	ErrGrantRejected = errors.New("oidc: authorization server rejected the token grant")

	// ErrMissingIDToken is returned when a token response has no error but
	// also carries no id_token.
	ErrMissingIDToken = errors.New("oidc: token response carries no id_token")

	// ErrIDTokenMalformed is returned when the ID token is not a
	// well-formed compact JWS, its header names an unrecognized field, or
	// its payload is not valid JSON carrying the claims this package
	// requires.
	ErrIDTokenMalformed = errors.New("oidc: id token is malformed")

	// ErrUnsupportedAlgorithm is returned when the ID token's header names
	// an algorithm outside internal/trust/federation's closed
	// RS256/ES256/EdDSA vocabulary, or names an algorithm this specific
	// issuer's own registered [issuerregistry.Issuer.Algorithms] set does
	// not declare -- the downgrade-resistance control: an issuer that only
	// ever declared ES256 never accepts an RS256- or EdDSA-signed token,
	// even one signed by a key this package could otherwise verify.
	ErrUnsupportedAlgorithm = errors.New("oidc: id token algorithm is not in the issuer's declared algorithm set")

	// ErrAlgorithmDowngrade is returned when the ID token header's
	// algorithm does not match the algorithm the resolved verification key
	// was published under -- the classic "swap the header's alg to
	// something the same key bytes can be coerced to verify under"
	// confusion attack.
	ErrAlgorithmDowngrade = errors.New("oidc: id token algorithm does not match the resolved key's algorithm")

	// ErrSignatureInvalid is returned when the ID token's signature does
	// not verify against the resolved key.
	ErrSignatureInvalid = errors.New("oidc: id token signature does not verify")

	// ErrKeyNotFound is returned when no currently valid signing key
	// resolves for the issuer and key id the ID token names -- including
	// an unsigned token (empty signature) and a token naming a key id this
	// issuer's registry entry never pinned.
	ErrKeyNotFound = errors.New("oidc: no verification key resolves for this issuer and key id")

	// ErrKeyExpired is returned when the resolved key exists but the ID
	// token's time falls outside that key's own NotBefore/NotAfter window.
	ErrKeyExpired = errors.New("oidc: verification key is outside its own validity window")

	// ErrWrongIssuer is returned when the ID token's iss claim does not
	// exactly match the issuer URL this flow began authorization with --
	// the issuer-confusion / mix-up-attack defense.
	ErrWrongIssuer = errors.New("oidc: id token issuer does not match the issuer this flow began with")

	// ErrWrongAudience is returned when the ID token's aud claim does not
	// contain the registered OAuth client id, or when aud names more than
	// one party and azp does not equal that client id.
	ErrWrongAudience = errors.New("oidc: id token audience does not match the registered OAuth client id")

	// ErrIDTokenExpired is returned when the ID token's validity window
	// (iat/exp, and nbf when present) does not cover the current time
	// within the issuer's configured clock-skew bound, including a
	// malformed window where exp does not follow iat.
	ErrIDTokenExpired = errors.New("oidc: id token validity window does not cover the current time")

	// ErrNonceMismatch is returned when the ID token's nonce claim does not
	// exactly equal the nonce this flow generated for the authorization
	// request it is completing -- the ID-token-substitution defense.
	ErrNonceMismatch = errors.New("oidc: id token nonce does not match the nonce this flow issued")

	// ErrAccessTokenMismatch is returned when the ID token declares an
	// at_hash claim and it does not match the access_token the same token
	// response returned -- the access-token-substitution defense.
	ErrAccessTokenMismatch = errors.New("oidc: id token at_hash does not match the returned access token")

	// ErrClaimMapping is returned when applying the issuer's governed
	// claim mappings to the verified ID token payload fails: a mapped
	// claim is present but the wrong JSON shape for its target field, an
	// enumerated field (sub_kind, assurance) names a value outside its
	// closed vocabulary, or no subject claim resolves at all.
	ErrClaimMapping = errors.New("oidc: issuer claim mapping produced an invalid principal field")
)
