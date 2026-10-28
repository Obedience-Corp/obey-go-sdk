# Client and run lifecycle

Construct with `NewClient(path, options...)` or `NewClientFromPath(options...)`.
Construction checks the executable and clones configuration without daemon or
provider calls. Each operation receives its caller context. `WithCamp` chooses
a default explicit scope; per-call Camp overrides it. For final-only runs without an explicit camp, the CLI infers
scope from child cwd (`WithWorkingDirectory`). Persistent handles and observation
use explicit camp/session identities. Agent workspace
(`SessionOptions.WorkingDirectory`, forwarded as `--workdir`) is separate.

Session defaults, JSON config, environment maps, per-call files and timeout values
are copied. Finish preparing an option before passing it; callers may reuse or
change their own options after StreamPrompt returns. The client configuration
is immutable and operations may run concurrently. Environment overrides replace
inherited keys in the child and never change the parent environment. Prompts are
passed byte-for-byte on stdin, never argv or a shell. Relative files resolve from
the child cwd under existing CLI attachment rules.

RunPrompt drains the complete stream and returns a typed result/error.
StreamPrompt prefers advertised run-v2 when an explicit camp and live daemon
`chat_surfaces`, `durable_turns` and `turn_identity` capabilities are available.
It reports live fidelity; successful output needs matching live completion plus
scoped authoritative completed state for the fresh first attempt. An advertised
typed receipt is required; older identity-capable daemons without that optional
receipt remain supported. Unsupported live requirements or absent explicit camp
select supported run-v1 with final-only fidelity when RequireLiveEvents is false.
RequireLiveEvents fails before creation if live requirements are unavailable.
There is no downgrade after a mutation starts.

Nil timeout keeps the CLI's 30-minute default; pointer zero disables it; negative
values fail before spawning. Caller cancellation always remains effective. Wire
timeout/canceled errors preserve the standard context cause even when the caller
context remains live. Doctor health failure does not invalidate a valid CLI
capability document; discovery uses a bounded 30-second child.

Use one consumer for Next/Result. Event and Result return independent snapshots;
Err returns a typed *Error even when Result is not called. Result drains unread
records and waits for child exit, then caches its validated result. Repeated
Result calls return copies. Concurrent Next/Result returns stream_misuse.

Cancellation signals only the dedicated CLI child, permits its cleanup for the
configured grace (default 10 seconds), then kills/reaps it. Result continues draining
during grace after Next returns false; it can retain confirmed stopped cleanup
and partial session identity with a cancellation error. Missing confirmation
remains cleanup unknown. Close is idempotent and safe concurrently with a blocked
Next; Client.Close releases its children and rejects new operations. The client
does not signal the shared daemon or independently stop persistent work.

Per-record output and final content have separate 64 MiB defaults; stderr retains
a 64 KiB tail and queues hold 16 records. Slow open consumers exert backpressure
without internal silent drops. Cancellation switches the same stdout reader to
an ordered shutdown spool, so unread RunStream.Close or Client.Close can let the
CLI perform owned cleanup. Already queued records precede spooled records; later
Result can still read diagnostics and cleanup confirmation. MaxShutdownBytes is
an independent retention budget, default 64 MiB when zero; negative is invalid.
Each record charges its payload bytes plus 64 bytes for metadata, bounding many
tiny records too. Overflow reports output_limit and continues draining without
retaining the remaining output for physical cleanup; missing terminal confirmation
stays unknown. ShutdownGrace remains ten seconds by default, above the CLI's
five-second owned Stop bound. No force-killed child implies cleanup success.

Linux real-daemon behavior is tested separately from native macOS lifecycle
certification. Windows builds but equivalent native graceful cleanup remains
unsupported/unverified. This local candidate does not claim publication.

## Persistent handles

`CreateSession` leaves the daemon session alive when the method, Client or
application exits. A successful create returns the canonical camp ID in its
`Session.Ref()`, including when `WithCamp` selected a camp by name. Per-call Camp
wins over `WithCamp`, which wins over session defaults. Child cwd remains
independent from the agent workspace. A creation error may return a known Session
alongside the error because adapter refusal or output loss can happen after
persistence. Save that reference and inspect the error; creation is never retried
automatically and closing the client never stops that session.

`OpenSession(ctx, SessionRef)` checks the exact returned camp/session scope.
`OpenTurn(ctx, TurnRef)` requires camp, session and turn IDs, reads authoritative
state, and returns a handle for completed, recovery-required, historical or
unknown states. It performs no send, reconciliation, resolution or revival.
Optional idempotency key/client reference metadata stays exactly as supplied;
a reopened handle has no acceptance receipt, and missing metadata is never
inferred from the stored prompt or turn. Serialize `Turn.Ref()` to reopen from a
new client or application process. `State(ctx)` performs another bounded read;
unknown statuses and raw fields remain available without claiming completion.

Structured failures retain daemon codes, gRPC details, process exit and copied raw
records, including additional scope and uncertainty fields. Close makes future
I/O methods return `closed_client` before option validation. Discovery refreshes
when the executable is replaced or a daemon gate/error invalidates it; failed
mutations are never retried to refresh discovery.

`Session.Send` submits once and returns a copied durable acceptance snapshot;
`Acceptance` describes acceptance, not turn completion. It never waits implicitly.
A queued receipt may have position zero. Persist your own idempotency key before
calling Send when recovery across an application crash matters; a nil options
value generates a cryptographic key that is retained in the returned reference or
error. Explicit same-key retries resolve the original accepted turn even when the
new prompt or model differs. Model applies to the accepted turn; Mode retains the
existing persistent session mode behavior.

After possible submission, missing/unsupported receipts, lost acknowledgement,
cancellation, and transport failure preserve the original key and uncertainty.
A structurally valid known turn ID can produce a partial Turn without a successful
Acceptance snapshot. Contradictory scope or request correlation never replaces a
trusted identity. Unknown valid CLI error codes remain available with conservative
acceptance uncertainty. No automatic mutation retry, replacement key, CancelTurn,
or Stop occurs. Closing the client or canceling the caller detaches; committed
work may continue and complete independently.

`Turn.Wait` performs read-only state and observation operations for the exact
original camp/session/turn. Its lifetime comes from the caller context; it adds no
30-minute timeout and never invokes `send --wait`. Individual reads and attach
establishment are bounded to 30 seconds. A marker triggers a state reread, and
periodic reads cover dropped completion notifications. A successor is exposed as
an observed identity while the result remains bound to the original target.
Every exit cancels and joins the private receiver and reaps its child. Independent
output-limit failures remain errors even when persisted completion is available.

Completed turns require a retained completion row for the latest completed
attempt, including attempts numbered above one. Empty content is a valid answer
only when that row exists. Wait captures a transcript high-water sequence, reads
ascending opaque pages through that boundary, selects only matching turn/attempt
completion content, and rechecks authoritative state before returning. Older
attempts, siblings, tool output, and reasoning cannot become the answer. Missing
retention or a disappearing cut returns `result_unavailable`; recovery returns
`recovery_required` without reconciliation or prompt replay. Results report
`retained_transcript`, `final_only`, the captured boundary, and `not_owned` cleanup.

`Session.Transcript` returns the daemon's opaque `NextCursor` and `HasMore`, exact
sequence integers, typed entries, copied raw entries and the copied full raw page.
Cursors belong to their original scope; no attach redirect reuses one for another
session. A page or marker does not establish complete historical replay.

## Rich events and observations

Event preserves copied RawRecord and RawPayload for the original protobuf
wrapper. Typed helpers cover completion, delta, tool call/result, approval,
acceptance, marker, backfill, history page and drop count. Reasoning and plan-exit
notices remain distinct from assistant content and real approval requests. Unknown
variants and enum values stay raw and never become successful completion evidence.
Positive durable sequences are exact integers; zero is unsequenced (nil pointer).
Repeated markers and zero/gapped sequences are not deduplication keys. Drop counts
report ephemeral loss; a marker or high-water value cannot establish full replay.

Session.Attach and Client.Observe return EventStream, which has Next/Event/Err/
Close and intentionally no Result because it spans turns. Construction validates
one first packet and preserves it for the first Next; establishment ends before
returning, independent of delayed consumption. Stream lifetime then comes only
from the caller context. Use one Next consumer; Close is safe concurrently and
only detaches. It never sends approval, CancelTurn or StopSession.

Attach exposes requested and actual observed identities without changing the
Session handle. A retained handle can observe a successor. OpenSession remains
strict: reopening an old reference after server resolution moves to a successor
returns scope_mismatch, rather than silently rebinding. A nonzero FromSequence
belongs to the original session; a redirect returns scope_mismatch with observed
identity so the caller can explicitly reopen the observed session and reset its
cursor. No reconnect, cursor migration or mutation replay is automatic.

Finite Observe with zero Tail uses 20; Follow with zero Tail skips recent history.
Cursor applies to a positive history tail and remains opaque. History has copied
raw entries/page plus NextCursor/HasMore; fetch subsequent pages explicitly.
Observe follow receives the daemon HELLO even for an idle camp, and unexpected EOF
of an open stream is incomplete_stream. See examples/observe for explicit history
pagination and a caller-controlled attach/reconnect cursor.

## Explicit controls

`Session.PendingApprovals(ctx)` reads only that handle's exact camp/session and
returns independent typed/raw snapshots with request/expiry timestamps. Snapshot
and live `Event.Approval` identify a request by `(SessionRef, CallID)`; applications
can collapse repeated observations by that identity, as the approvals example does.
A plan-exit notice remains an activity, never an approval request.

`Session.RespondApproval(ctx, ApprovalDecision{CallID: call, Approve: true, Reason:
reason})` sends one explicit decision. Set `Approve: false` for denial. There is no
policy callback or implicit affirmative default. Every acknowledgement frame must
carry the exact requested call ID. Expired, resolved, stale and wrong-scope refusals
retain their original CLI/daemon codes and raw details. If the caller cancels after
submission or the acknowledgement is malformed/lost, `Error.DecisionIndeterminate`
can be true. Inspect durable state; the SDK never retries a decision automatically.
A timeout/canceled wire error retains the standard context cause through errors.Is.

`Turn.Cancel(ctx)` targets the handle's original camp/session/turn; it does not pick
a current turn or stop the session. `CancelResult.Status` is the raw acknowledged
canonical status; `State` is optional because an older/current server may return
only status. When present, the protojson state is scope-checked, preserves exact
integers and retains its original raw object. An acknowledgement can precede
physical provider exit. Cancellation at the existing pre-stream gate can report
`TURN_FAILED`; after dispatch it can return `recovery_required` with an already
recorded provider effect. Cancellation does not undo that effect or permit replay.
Canceling an already completed turn returns its completed status.

`Session.Stop(ctx)` issues one explicit target stop. The daemon retains its existing
managed-child custody cleanup; independent sessions are not selected. Timeout or
lost acknowledgement remains an error and may require authoritative readback.
Closing a persistent observer/client or canceling Wait only detaches the caller;
these explicit control methods are the paths that request cancellation/stop.

The approvals example lists or watches without deciding. To act, supply both
`--call CALL --decision approve` (or `deny`), with explicit camp/session IDs. No
example infers a decision from tool name or from a plan-exit notice.
