package axi

// suggest.go holds the contextual next-step hint table (AXI §9 Contextual
// disclosure). Hints are complete commands or templates — never "see --help" —
// and carry placeholders (<id>, "<title>") for runtime values so they never
// mislead with a guessed concrete value.
//
// Commands build most hints inline (they know the exact situation), but the
// error-path hints come from here so error translation and remediation stay in
// one place.

// HintsForCode returns the next-step hints to attach to an error with the
// given machine code. Returns nil for codes with no generic remediation.
func HintsForCode(code string) []string {
	switch code {
	case CodeNotFound:
		return []string{"Run `bkt-axi pr list` to see available pull requests"}
	case CodeAuthRequired:
		return []string{
			"Run `bkt-axi auth login` to authenticate",
			"Run `bkt-axi auth status` to inspect configured hosts",
		}
	case CodeForbidden:
		return []string{
			"Run `bkt-axi auth status` to check your token and its scope",
			"Confirm the active context has access to this repository",
		}
	case CodeRateLimited:
		return []string{"Wait a moment and retry; Bitbucket is throttling requests"}
	case CodeUnavailable:
		return []string{"Retry shortly; Bitbucket returned a server error"}
	}
	return nil
}

// AfterPRList returns hints to show after a non-empty pr list. When more items
// exist beyond the returned page, the first hint tells the agent how to see
// the rest.
func AfterPRList(moreAvailable bool) []string {
	if moreAvailable {
		return []string{"Raise --limit to see more results"}
	}
	return []string{"Run `bkt-axi pr view <id>` for a pull request's details"}
}

// EmptyPRList returns the definitive empty-state hint for pr list.
func EmptyPRList() []string {
	return []string{
		"Run `bkt-axi pr list --state all` to include merged and declined pull requests",
		"Run `bkt-axi pr list --reviewer <you>` to review pull requests assigned to you",
	}
}

// AfterPRView returns hints to show after a pr detail view.
func AfterPRView(id int) []string {
	return []string{
		"Run `bkt-axi pr view " + itoa(id) + " --comments` to read the comment thread",
	}
}

// AfterAuthStatus returns hints for the auth status view.
func AfterAuthStatus(configured int) []string {
	if configured == 0 {
		return []string{"Run `bkt-axi auth login` to add a host"}
	}
	return []string{"Run `bkt-axi pr list` to act on pull requests"}
}

func itoa(n int) string {
	const digits = "0123456789"
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = digits[n%10]
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
