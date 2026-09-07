package protocol

import (
	"encoding/json"
	"reflect"
	"slices"

	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/theme"
)

// Execution determines ownership and whether an operation enters the journal.
type Execution string

const (
	Query        Execution = "query"
	Command      Execution = "command"
	Ephemeral    Execution = "ephemeral"
	Subscription Execution = "subscription"
	Lifecycle    Execution = "lifecycle"
)

// Operation is the source of truth for generated client contracts. Permission
// describes existing runtime checks; it never asserts an authenticated client.
type Operation struct {
	Name       string       `json:"name"`
	Surface    string       `json:"surface"`
	Execution  Execution    `json:"execution"`
	Permission string       `json:"permission"`
	Sensitive  bool         `json:"sensitive,omitempty"`
	Params     reflect.Type `json:"-"`
	Result     reflect.Type `json:"-"`
}

type Empty struct{}
type Accepted struct {
	Accepted bool `json:"accepted"`
}
type PingResult struct {
	Generation int64  `json:"generation,string"`
	BuildID    string `json:"build_id"`
}
type EventNotification struct {
	Event ProtocolEvent `json:"event"`
}
type SubscriptionFailure struct {
	SubscriptionID string    `json:"subscription_id"`
	RootID         string    `json:"root_id"`
	Error          *RPCError `json:"error"`
}

type HistoryPageParams struct {
	RootID     string `json:"root_id"`
	AgentID    string `json:"agent_id"`
	AfterSeq   int    `json:"after_seq,omitempty"`
	BeforeSeq  int    `json:"before_seq,omitempty"`
	ThroughSeq int    `json:"through_seq"`
	Revision   *int64 `json:"revision,omitempty,string"`
	Limit      int    `json:"limit"`
	MaxBytes   int    `json:"max_bytes"`
	Recent     bool   `json:"recent,omitempty"`
}

func rpc[P, R any](name string, execution Execution, permission string, sensitive bool) Operation {
	return Operation{Name: name, Surface: "rpc", Execution: execution, Permission: permission, Sensitive: sensitive,
		Params: reflect.TypeFor[P](), Result: reflect.TypeFor[R]()}
}

var rpcOperations = []Operation{
	rpc[HostDirectoryParams, HostDirectoryResult]("host.directories.list", Query, "host-runtime", false),
	rpc[HostAttentionParams, HostAttentionResult]("host.attention", Query, "host-runtime", false),
	rpc[EmptyParams, theme.CatalogResult]("host.themes.list", Query, "host-runtime", false),
	rpc[HostThemeResolveParams, theme.Resolved]("host.themes.resolve", Query, "host-runtime", false),
	rpc[MailboxPageParams, session.MailboxPage]("mailbox.list", Query, "root-agent-association", false),
	rpc[MailboxReadParams, session.MailboxInspection]("mailbox.read", Query, "root-agent-association", false),
	rpc[CompletionParams, CompletionResult]("workspace.complete", Query, "root-agent-association", false),
	rpc[SessionCatalogParams, session.SessionCatalogPage]("sessions.list", Query, "host-runtime", false),
	rpc[SessionSummariesParams, SessionSummariesResult]("sessions.summaries", Query, "host-runtime", false),
	rpc[EmptyParams, session.CatalogRevision]("sessions.revision", Query, "host-runtime", false),
	rpc[RootCollectionParams, session.RootCollectionPage]("root.collection", Query, "root-association", false),
	rpc[ContentReadParams, ContentReadResult]("content.read", Query, "root-agent-content-grant", false),
	rpc[QueryParams, QueryResult]("operation.invoke", Ephemeral, "operation-specific", true),
	rpc[QueryParams, QueryResult]("query", Query, "operation-specific", false),
	rpc[Empty, ProviderLoginList]("provider.login.list", Query, "host-configuration", false),
	rpc[ProviderNameParams, ProviderStatus]("provider.status", Query, "host-configuration", false),
	rpc[ProviderNameParams, ProviderStatus]("provider.logout", Ephemeral, "host-configuration", false),
	rpc[ProviderNameParams, ProviderStatus]("provider.key.rotate", Ephemeral, "host-configuration", true),
	rpc[InitializeParams, InitializeResult]("initialize", Query, "none", false),
	rpc[Empty, PingResult]("daemon.ping", Query, "none", false),
	rpc[CommandParams, CommandResult]("command.submit", Command, "operation-specific", false),
	rpc[CommandStatusParams, CommandResult]("command.status", Query, "client-command-namespace", false),
	rpc[SubscribeParams, SubscribeResult]("events.subscribe", Subscription, "root-association", false),
	rpc[UnsubscribeParams, Empty]("events.unsubscribe", Subscription, "connection-subscription", false),
	rpc[ReplayParams, ReplayResult]("events.replay", Query, "root-association", false),
	rpc[SnapshotParams, session.RootSnapshot]("root.snapshot", Query, "root-association", false),
	rpc[HistoryPageParams, session.BoundedTranscriptPage]("history.page", Query, "root-agent-association", false),
	rpc[ProviderValidateParams, ProviderValidateResult]("provider.validate", Ephemeral, "host-configuration", true),
	rpc[UploadBeginParams, Accepted]("upload.begin", Ephemeral, "content-grant", false),
	rpc[UploadChunkParams, Accepted]("upload.chunk", Ephemeral, "connection-upload", false),
	rpc[UploadFinishParams, ContentHandle]("upload.finish", Ephemeral, "content-grant", false),
	rpc[PermissionDecisionParams, PermissionDecisionResult]("permission.decide", Ephemeral, "trusted-client-decision", false),
	rpc[Empty, RuntimeConfiguration]("config.get", Query, "none", false),
	rpc[ConfigurationUpdate, RuntimeConfiguration]("config.update", Ephemeral, "configuration-revision", false),
	rpc[ProviderKeySetup, RuntimeConfiguration]("provider.key.set", Ephemeral, "configuration-revision", true),
	rpc[Empty, ProviderLoginStatus]("provider.login.begin", Ephemeral, "host-configuration", false),
	rpc[ProviderLoginParams, ProviderLoginStatus]("provider.login.status", Query, "none", false),
	rpc[ProviderLoginParams, ProviderLoginStatus]("provider.login.cancel", Ephemeral, "host-configuration", false),
	rpc[ProviderLoginTeamParams, ProviderLoginStatus]("provider.login.team.select", Ephemeral, "host-configuration", false),
	rpc[ProviderLoginProjectParams, ProviderLoginStatus]("provider.login.project.select", Ephemeral, "host-configuration", false),
	rpc[ProviderLoginCreateParams, ProviderLoginStatus]("provider.login.project.create", Ephemeral, "host-configuration", false),
	rpc[RestartParams, Empty]("daemon.restart", Lifecycle, "armed-generation", false),
	rpc[RestartParams, Empty]("daemon.stop", Lifecycle, "armed-generation", false),
}

// Operations returns deterministic metadata without exposing mutable registry state.
func Operations() []Operation {
	result := append(slices.Clone(rpcOperations), runtimeOperations...)
	slices.SortFunc(result, func(a, b Operation) int {
		if a.Surface != b.Surface {
			if a.Surface < b.Surface {
				return -1
			}
			return 1
		}
		if a.Name < b.Name {
			return -1
		}
		if a.Name > b.Name {
			return 1
		}
		return 0
	})
	return result
}

func Lookup(name string) (Operation, bool) {
	for _, operation := range Operations() {
		if operation.Name == name {
			return operation, true
		}
	}
	return Operation{}, false
}

func Events() map[string]reflect.Type {
	return map[string]reflect.Type{
		"event":               reflect.TypeFor[EventNotification](),
		"subscription.failed": reflect.TypeFor[SubscriptionFailure](),
	}
}

func LookupRuntime(name string) (Operation, bool) {
	for _, operation := range runtimeOperations {
		if operation.Name == name {
			return operation, true
		}
	}
	return Operation{}, false
}

type QueryParams struct {
	RootID    string          `json:"root_id,omitempty"`
	Operation string          `json:"operation"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}
type QueryResult struct {
	Result  json.RawMessage `json:"result,omitempty"`
	Content *ContentHandle  `json:"content,omitempty"`
	RootID  string          `json:"root_id,omitempty"`
}
