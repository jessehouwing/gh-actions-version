// Package actions holds the remediation actions (port of lib/actions/**).
// Constructors are the contract used by the rules; Execute/ManualCommands are implemented per action file.
package actions

import (
	"context"

	"github.com/jessehouwing/gh-version-action/internal/state"
)

// Base carries the shared RemediationAction fields.
type Base struct {
	Desc     string
	Ver      string
	Prio     int
	Language string // manual command fence language, default "bash"
}

func (b *Base) Description() string { return b.Desc }
func (b *Base) Version() string     { return b.Ver }
func (b *Base) Priority() int       { return b.Prio }
func (b *Base) ManualCommandsLanguage() string {
	if b.Language == "" {
		return "bash"
	}
	return b.Language
}

func newBase(desc, version string, prio int) Base { return Base{Desc: desc, Ver: version, Prio: prio} }

// issueFor finds the issue owned by action a so Execute can update its status/message.
func issueFor(st *state.State, a state.Action) *state.Issue {
	for _, i := range st.Issues {
		if i.Action == a {
			return i
		}
	}
	return nil
}

var _ = context.Background
