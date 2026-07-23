package app

import (
	"github.com/ruttybob/bkt-axi/internal/axi"
)

// exit.go documents and re-exports the AXI exit-code contract used by the
// dispatcher. The mapping itself (0 success incl. no-op, 1 error, 2 usage)
// lives in internal/axi.ExitCode and is applied in App.Run; this file keeps
// the constants visible at the app layer and documents the contract in one
// place.
//
// ExitSuccess (0): the command succeeded, including idempotent no-ops where
//
//	the desired state already held (approve/merge of an already-approved PR).
//
// ExitError   (1): a runtime error — the command could not be satisfied
//
//	(not found, auth required, network, …).
//
// ExitUsage   (2): a usage error — unknown flag, missing required argument,
//
//	unknown command. The agent's deterministic next move is to re-read the
//	inline valid-flag list or run <command> --help.
const (
	ExitSuccess = axi.ExitSuccess
	ExitError   = axi.ExitError
	ExitUsage   = axi.ExitUsage
)
