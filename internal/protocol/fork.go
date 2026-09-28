package protocol

import (
	"time"

	"github.com/context-labs/whip/internal/session"
)

type ForkParams struct {
	ForkID                  ID      `json:"fork_id"`
	SessionID               ID      `json:"session_id"`
	ExpectedHistoryRevision Counter `json:"expected_history_revision"`
	ExpectedConfigRevision  Counter `json:"expected_config_revision"`
	ObservedThrough         Counter `json:"observed_through"`
	KeepThrough             Counter `json:"keep_through"`
	Title                   *string `json:"title" maxLength:"1024"`
}

type Fork struct {
	ID                      ID      `json:"id"`
	SessionID               ID      `json:"session_id"`
	ExpectedHistoryRevision Counter `json:"expected_history_revision"`
	ExpectedConfigRevision  Counter `json:"expected_config_revision"`
	ObservedThrough         Counter `json:"observed_through"`
	KeepThrough             Counter `json:"keep_through"`
	Title                   *string `json:"title"`
	TreeID                  ID      `json:"tree_id"`
	RootID                  ID      `json:"root_id"`
	CreatedAt               string  `json:"created_at"`
}

// The receipt remains immutable. Tree and Root are current projections and are
// null after destination deletion; an exact retry never recreates that tree.
type ForkResult struct {
	Fork    Fork     `json:"fork"`
	Tree    *Tree    `json:"tree"`
	Root    *Session `json:"root"`
	Deleted bool     `json:"deleted"`
}

func ForkResultFromDomain(value session.ForkResult) (ForkResult, error) {
	f := value.Fork
	result := ForkResult{Fork: Fork{
		ID: ID(f.ID), SessionID: ID(f.SessionID),
		ExpectedHistoryRevision: Counter(f.ExpectedHistoryRevision), ExpectedConfigRevision: Counter(f.ExpectedConfigRevision),
		ObservedThrough: Counter(f.ObservedThrough), KeepThrough: Counter(f.KeepThrough), Title: f.Title,
		TreeID: ID(f.TreeID), RootID: ID(f.RootID), CreatedAt: f.CreatedAt.Format(time.RFC3339Nano),
	}, Deleted: value.Deleted}
	if value.Tree != nil {
		result.Tree = new(TreeFromDomain(*value.Tree))
	}
	if value.Root != nil {
		root, err := SessionFromDomain(*value.Root)
		if err != nil {
			return ForkResult{}, err
		}
		result.Root = &root
	}
	return result, nil
}
