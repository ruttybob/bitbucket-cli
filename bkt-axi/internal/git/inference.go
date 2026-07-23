package git

// inference.go provides best-effort git inference for the mutation commands
// (pr create's --source/--title defaults). Every helper degrades to "" on any
// error so a command never fails solely because inference was unavailable; the
// command surfaces a clean usage error asking for the missing value instead.

// CurrentBranch returns the abbreviated name of HEAD's branch in dir ("" →
// current directory), or "" when dir is not a git repository / HEAD is
// detached.
func CurrentBranch(dir string) string {
	out, err := Run(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	name := trimSpace(string(out))
	if name == "" || name == "HEAD" {
		return ""
	}
	return name
}

// LastCommitSubject returns the subject line of HEAD's commit in dir, or "" on
// any error. Used as the default PR title.
func LastCommitSubject(dir string) string {
	out, err := Run(dir, "log", "-1", "--pretty=%s")
	if err != nil {
		return ""
	}
	return trimSpace(string(out))
}
