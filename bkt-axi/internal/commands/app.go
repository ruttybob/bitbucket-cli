package commands

import (
	"github.com/ruttybob/bkt-axi/internal/app"
)

// app.go wires the full Phase 0 command tree into the dispatcher. Later phases
// add more nouns (repo, branch, commit, pipeline, …) and verbs here; the
// dispatcher and adapter layer are already shaped for them.

// Version is the binary version, overridable via -ldflags at release time.
var Version = "0.1.0-dev"

// NewApp builds the configured App: identity, the command tree, and the
// content-first home view.
func NewApp(binPath string) *app.App {
	a := &app.App{
		Name:        "bkt-axi",
		Description: "Bitbucket Cloud and Data Center CLI for agents",
		BinPath:     binPath,
		Version:     Version,
		Commands: []*app.Command{
			NewPRCmd(),
			NewAuthCmd(),
			NewRepoCmd(),
			NewBranchCmd(),
			NewCommitCmd(),
			NewPipelineCmd(),
			NewIssueCmd(),
			NewWebhookCmd(),
			NewVariableCmd(),
			NewProjectCmd(),
			NewStatusCmd(),
			NewPermsCmd(),
			NewAdminCmd(),
			NewAPICmd(),
			NewContextCmd(),
		},
	}
	a.Home = RunHome
	return a
}
