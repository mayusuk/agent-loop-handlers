// Package compat records which handler contract versions this release
// supports (INTEGRATION-SPEC §2, §12.4).
package compat

import "github.com/mayusuk/agent-loop/contract"

// Contract is the contract version these handlers were written for.
const Contract = contract.Version
