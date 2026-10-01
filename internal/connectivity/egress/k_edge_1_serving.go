package egress

import (
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
)

// ValidateServingContract checks the immutable gateway boundary that a
// serving cell relies on before it can compose provider adapters. It is
// deliberately read-only: configuration and network effects belong to the
// composition root and every request still passes through Gateway.Do.
func ValidateServingContract() error {
	for _, method := range []string{
		http.MethodGet, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodHead,
	} {
		if !allowedEgressMethod(method, false) {
			return fmt.Errorf("egress: serving contract refused method %q", method)
		}
	}
	for _, method := range []string{http.MethodTrace, http.MethodOptions, http.MethodConnect} {
		if allowedEgressMethod(method, false) {
			return fmt.Errorf("egress: serving contract admitted method %q", method)
		}
	}
	for _, address := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "100.100.100.200"} {
		if !unsafeAddress(netip.MustParseAddr(address)) {
			return fmt.Errorf("egress: serving contract admitted unsafe address %s", address)
		}
	}
	valid, _ := url.Parse("https://provider.example.test/v1")
	if err := validateURL(valid); err != nil {
		return fmt.Errorf("egress: serving contract rejected TLS URL: %w", err)
	}
	invalid, _ := url.Parse("http://provider.example.test/v1")
	if err := validateURL(invalid); err == nil {
		return fmt.Errorf("egress: serving contract admitted a non-TLS URL")
	}
	return nil
}
