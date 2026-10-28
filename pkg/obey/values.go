package obey

import (
	"encoding/json"
	"time"
)

type ProtocolVersion struct {
	Name    string
	Version int
}
type SessionRef struct {
	CampID    string `json:"camp_id"`
	SessionID string `json:"session_id"`
}
type TurnRef struct {
	SessionRef
	TurnID         string `json:"turn_id"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
	ClientRef      string `json:"client_ref,omitempty"`
}
type TurnStatus string // canonical TURN_*; unknown strings/numeric enum text retained
const (
	TurnQueued            TurnStatus = "TURN_QUEUED"
	TurnRunning           TurnStatus = "TURN_RUNNING"
	TurnCompleted         TurnStatus = "TURN_COMPLETED"
	TurnFailed            TurnStatus = "TURN_FAILED"
	TurnCanceled          TurnStatus = "TURN_CANCELLED"
	TurnRecoveryRequired  TurnStatus = "TURN_RECOVERY_REQUIRED"
	TurnHistoricalUnknown TurnStatus = "TURN_HISTORICAL_UNKNOWN"
)

type Acceptance struct {
	Raw           json.RawMessage // Original accepted-turn payload, copied on return.
	Ref           TurnRef
	Status        TurnStatus
	QueuePosition *uint32
	Deduplicated  bool
}
type TurnState struct {
	Ref                                              TurnRef
	Status                                           TurnStatus
	Version                                          int64
	QueuePosition                                    *uint32
	RequestedProvider, RequestedModel, RequestedMode string
	LatestAttemptID                                  string
	RecoveryRequired                                 bool
	RecoveryReason                                   string
	Usage                                            Usage
	ErrorCode, ErrorMessage                          string
	Raw                                              json.RawMessage
}
type StreamFidelity string

const (
	FidelityFinalOnly StreamFidelity = "final_only"
	FidelityLive      StreamFidelity = "live"
)

// Limits default to 64 MiB record/result/shutdown, 64 KiB stderr, queue 16 and shutdown 10s.
// Zero selects the default; negative values are invalid.
type Limits struct {
	// MaxShutdownBytes bounds cancellation-only retained records: payload bytes
	// plus 64 bytes per record. Zero defaults to 64 MiB; negative is invalid.
	MaxShutdownBytes int64
	MaxRecordBytes   int64
	MaxResultBytes   int64
	StderrTailBytes  int
	EventQueueSize   int
	ShutdownGrace    time.Duration
}

type SessionOptions struct {
	Provider, Model, Camp, WorkingDirectory, Agent, AgentProfile, Mode, ReasoningEffort, FestivalID, TaskID string
	Config                                                                                                  json.RawMessage
}
type RunOptions struct {
	SessionOptions
	Files             []string
	Timeout           *time.Duration
	RequireLiveEvents bool
}
type SendOptions struct {
	// Mode persists on the session; Model applies only to this accepted turn.
	Mode, Model, ClientRef, IdempotencyKey string
	Files                                  []string
}
type AttachOptions struct {
	FromSequence  uint64
	BackfillLimit uint32
	NoBackfill    bool
}
type TranscriptOptions struct {
	Cursor   string
	PageSize uint32
}
type ObserveOptions struct {
	Camp       string
	SessionIDs []string
	Follow     bool
	Tail       uint32
	Cursor     string
}
type Usage struct {
	InputTokens, OutputTokens, CachedTokens int64
	Latency                                 time.Duration
	Complete                                *bool
	CostUSDTicks                            *int64
	CostIsPartial                           *bool
}
type CleanupDisposition string

const (
	CleanupNotOwned   CleanupDisposition = "not_owned"
	CleanupStopped    CleanupDisposition = "stopped"
	CleanupStopFailed CleanupDisposition = "stop_failed"
	CleanupUnknown    CleanupDisposition = "unknown"
)

type Result struct {
	OutputSource, OutputFidelity          string
	HighWaterSequence                     *uint64
	RequestedSessionID, ObservedSessionID string
	Text                                  string
	Session                               SessionRef
	TurnID                                string
	Usage                                 Usage
	Protocol                              ProtocolVersion
	Cleanup                               CleanupDisposition
	Diagnostics                           []Diagnostic
}
type Diagnostic struct {
	Code, Message, Severity string
	DaemonCode              *int32
	Details                 json.RawMessage
}
type Error struct {
	validatedCLIRecord bool // Set only from a fully validated failed CLI invocation.
	// RawRecord preserves additional CLI scope/uncertainty fields without parsing
	// diagnostic text. Returned raw values are copies owned by the caller.
	RawRecord                                                   json.RawMessage
	RequestedSessionID, ObservedSessionID, TargetTurnID, CallID string
	DecisionIndeterminate                                       bool
	Code, Operation                                             string
	Protocol                                                    ProtocolVersion
	Session                                                     SessionRef
	TurnID, IdempotencyKey, ClientRef                           string
	DaemonCode                                                  *int32
	GRPCCode                                                    string
	ExitCode                                                    *int
	StderrTail                                                  string
	Details                                                     json.RawMessage
	Diagnostics                                                 []Diagnostic
	AcceptanceIndeterminate                                     bool
	Cause                                                       error
}

// Implement Error() and Unwrap() preserving errors.Is context and errors.As process causes.
type EventKind string

const (
	EventUnknown        EventKind = "unknown"
	EventSessionCreated EventKind = "session_created"
	EventError          EventKind = "error"
	EventTerminal       EventKind = "terminal"
	EventCompletion     EventKind = "completion"
	EventDelta          EventKind = "message_delta"
	EventToolCall       EventKind = "tool_call"
	EventToolResult     EventKind = "tool_result"
	EventApproval       EventKind = "approval"
	EventReceipt        EventKind = "receipt"
	EventMarker         EventKind = "marker"
	EventBackfill       EventKind = "backfill"
	EventActivity       EventKind = "activity"
	EventDrop           EventKind = "drop"
)

type ActivityHistoryEvent struct {
	ID                                                                     string
	Session                                                                SessionRef
	GroupID                                                                string
	Sequence                                                               uint64
	Kind, Message, ActivityType, OldStatus, NewStatus, Metadata, Timestamp string
	Raw                                                                    json.RawMessage
}
type ActivityHistoryPage struct {
	Entries    []ActivityHistoryEvent
	NextCursor string
	HasMore    bool
	Raw        json.RawMessage
}
type Event struct {
	ObservedSessionID, TargetTurnID, CallID, OutputSource, OutputFidelity string
	DecisionIndeterminate                                                 bool
	HighWaterSequence                                                     *uint64
	History                                                               *ActivityHistoryPage
	Kind                                                                  EventKind
	Protocol                                                              ProtocolVersion
	PayloadType                                                           string
	Session                                                               SessionRef
	RequestedSessionID                                                    string
	TurnID                                                                string
	DurableSequence                                                       *uint64
	RawRecord, RawPayload                                                 json.RawMessage
	Completion                                                            *Completion
	Delta                                                                 *MessageDelta
	ToolCall                                                              *ToolCall
	ToolResult                                                            *ToolResult
	Approval                                                              *Approval
	Receipt                                                               *Acceptance
	Marker                                                                *AttachMarker
	Backfill                                                              *TranscriptEntry
	DroppedCount                                                          *int64
	Diagnostic                                                            *Diagnostic
	Terminal                                                              *Terminal
}
type Completion struct {
	Text, Model, StopReason string
	Usage                   Usage
}
type MessageDelta struct {
	Text        string
	ContentKind string
}
type ToolCall struct{ CallID, Name, Arguments string }
type ToolResult struct {
	CallID, Content, Error string
	Success                *bool
}
type Terminal struct {
	Status, Outcome string
	Cleanup         CleanupDisposition
}
type Approval struct {
	Session                     SessionRef
	CallID, ToolName, Arguments string
	RequestedAt, ExpiresAt      *time.Time
	Raw                         json.RawMessage
}
type ApprovalDecision struct {
	CallID  string
	Approve bool
	Reason  string
}
type AttachMarker struct {
	Type              string
	HighWaterSequence uint64
	CurrentTurnID     string
	CurrentTurnStatus TurnStatus
	CurrentAttemptID  string
	Queue             []QueuedTurn
	RecoveryRequired  bool
	RecoveryReason    string
	SnapshotVersion   int64
	PendingApprovals  []Approval
}
type QueuedTurn struct {
	TurnID             string
	Position           uint32
	Status             TurnStatus
	Version            int64
	ClientRef, Preview string
}
type TranscriptEntry struct {
	ActivityType                                                         string
	ID                                                                   string
	Session                                                              SessionRef
	Sequence                                                             uint64
	Timestamp                                                            *time.Time
	Actor, Kind, Content, ToolName, ToolCallID, Error, TurnID, AttemptID string
	Success                                                              *bool
	Metadata                                                             json.RawMessage
	Raw                                                                  json.RawMessage
}
type TranscriptPage struct {
	Raw        json.RawMessage
	Entries    []TranscriptEntry
	NextCursor string
	HasMore    bool
}
type CancelResult struct {
	Status TurnStatus // Acknowledged disposition; physical exit may follow.
	State  *TurnState // Optional scope-checked protojson snapshot, if supplied.
}
type Capabilities struct {
	Schema        string
	ReportVersion int
	Protocols     map[string]ProtocolSupport
	HealthStatus  string
	Diagnostics   []Diagnostic
	Raw           json.RawMessage
}
type ProtocolSupport struct {
	Protocol string
	Versions []int
}
type DaemonStatus struct {
	Version, Hostname   string
	CapabilitiesPresent bool
	Features            map[string]bool
	Raw                 json.RawMessage
}
