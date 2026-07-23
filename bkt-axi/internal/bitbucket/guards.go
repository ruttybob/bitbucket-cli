package bitbucket

import "fmt"

// guards.go centralizes the host-kind precondition errors so adapter methods
// emit one consistent, agent-readable sentence per platform mismatch instead
// of a raw "unsupported host kind". CloudOnly is the established helper
// (introduced for pipelines); DCOnly is its Data Center mirror.

// CloudOnly returns a structured error explaining that a Cloud-only command
// cannot run against the active host. otherName is the human label of the
// unsupported host (e.g. "Bitbucket Data Center") — pass c.hostKindLabel().
func CloudOnly(feature, otherName string) error {
	return fmt.Errorf("%s is Bitbucket Cloud only; the active host is %s", feature, otherName)
}

// DCOnly is the Data Center mirror of CloudOnly: it reports that a DC-only
// command cannot run against a Cloud host.
func DCOnly(feature, otherName string) error {
	return fmt.Errorf("%s is Bitbucket Data Center only; the active host is %s", feature, otherName)
}
