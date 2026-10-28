# Client and run lifecycle

Construct with `NewClient(path, options...)` or `NewClientFromPath(options...)`.
Construction checks the executable and clones configuration without daemon or
provider calls. Each operation receives its caller context. `WithCamp` chooses
a default explicit scope; per-call Camp overrides it. Otherwise the CLI infers
scope from child cwd (`WithWorkingDirectory`). Agent workspace
(`SessionOptions.WorkingDirectory`, forwarded as `--workdir`) is separate.

Session defaults, JSON config, environment maps, per-call files and timeout values
are copied. Finish preparing an option before passing it; callers may reuse or
change their own options after StreamPrompt returns. The client configuration
is immutable and operations may run concurrently. Environment overrides replace
inherited keys in the child and never change the parent environment. Prompts are
passed byte-for-byte on stdin, never argv or a shell. Relative files resolve from
the child cwd under existing CLI attachment rules.

RunPrompt drains the complete stream and returns a typed result/error.
StreamPrompt currently selects the advertised legacy run-v1 invocation without
`--protocol-version`, and reports final-only fidelity. RequireLiveEvents rejects
v1 before creating a session. Nil timeout keeps the CLI’s 30-minute default; pointer
zero disables it; negative values fail before spawning. Caller cancellation
always remains effective. Doctor health failure does not invalidate a valid CLI
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

Per-record output and final content have separate 64 MiB defaults; stderr retains a
64 KiB tail and queues hold 16 records. Overflow is an error, not truncation. Slow
consumers exert backpressure. During cancellation, abandoned queues are released
at the grace boundary; no cleanup success is inferred from a killed child.
Windows builds, but equivalent native graceful cleanup is not yet supported or
verified. This baseline does not claim durable session/turn APIs or publication.

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
