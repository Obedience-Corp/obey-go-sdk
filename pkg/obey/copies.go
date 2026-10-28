package obey

import "encoding/json"

func ptrCopy[T any](p *T) *T {
	if p == nil {
		return nil
	}
	value := *p
	return &value
}
func cloneUsage(u Usage) Usage {
	u.Complete = ptrCopy(u.Complete)
	u.CostUSDTicks = ptrCopy(u.CostUSDTicks)
	u.CostIsPartial = ptrCopy(u.CostIsPartial)
	return u
}
func cloneDiagnostic(d Diagnostic) Diagnostic {
	d.DaemonCode = ptrCopy(d.DaemonCode)
	d.Details = append(json.RawMessage(nil), d.Details...)
	return d
}
func cloneDiagnostics(in []Diagnostic) []Diagnostic {
	if in == nil {
		return nil
	}
	out := make([]Diagnostic, len(in))
	for i, d := range in {
		out[i] = cloneDiagnostic(d)
	}
	return out
}
func cloneResult(r *Result) *Result {
	if r == nil {
		return nil
	}
	out := *r
	out.Usage = cloneUsage(out.Usage)
	out.Diagnostics = cloneDiagnostics(out.Diagnostics)
	return &out
}
func cloneError(err error) error {
	e, ok := err.(*Error)
	if !ok {
		return err
	}
	out := *e
	out.DaemonCode = ptrCopy(out.DaemonCode)
	out.ExitCode = ptrCopy(out.ExitCode)
	out.Details = append(json.RawMessage(nil), out.Details...)
	out.Diagnostics = cloneDiagnostics(out.Diagnostics)
	return &out
}
func cloneApproval(a Approval) Approval {
	a.RequestedAt = ptrCopy(a.RequestedAt)
	a.ExpiresAt = ptrCopy(a.ExpiresAt)
	a.Raw = append(json.RawMessage(nil), a.Raw...)
	return a
}
func cloneTranscript(t TranscriptEntry) TranscriptEntry {
	t.Timestamp = ptrCopy(t.Timestamp)
	t.Success = ptrCopy(t.Success)
	t.Metadata = append(json.RawMessage(nil), t.Metadata...)
	t.Raw = append(json.RawMessage(nil), t.Raw...)
	return t
}
func cloneEvent(e Event) Event {
	e.RawRecord = append(json.RawMessage(nil), e.RawRecord...)
	e.RawPayload = append(json.RawMessage(nil), e.RawPayload...)
	e.DurableSequence = ptrCopy(e.DurableSequence)
	e.DroppedCount = ptrCopy(e.DroppedCount)
	e.Completion = ptrCopy(e.Completion)
	if e.Completion != nil {
		e.Completion.Usage = cloneUsage(e.Completion.Usage)
	}
	e.Delta = ptrCopy(e.Delta)
	e.ToolCall = ptrCopy(e.ToolCall)
	e.ToolResult = ptrCopy(e.ToolResult)
	if e.ToolResult != nil {
		e.ToolResult.Success = ptrCopy(e.ToolResult.Success)
	}
	e.Approval = ptrCopy(e.Approval)
	if e.Approval != nil {
		*e.Approval = cloneApproval(*e.Approval)
	}
	e.Receipt = ptrCopy(e.Receipt)
	if e.Receipt != nil {
		e.Receipt.QueuePosition = ptrCopy(e.Receipt.QueuePosition)
	}
	e.Marker = ptrCopy(e.Marker)
	if e.Marker != nil {
		e.Marker.Queue = append([]QueuedTurn(nil), e.Marker.Queue...)
		e.Marker.PendingApprovals = append([]Approval(nil), e.Marker.PendingApprovals...)
		for i, a := range e.Marker.PendingApprovals {
			e.Marker.PendingApprovals[i] = cloneApproval(a)
		}
	}
	e.Backfill = ptrCopy(e.Backfill)
	if e.Backfill != nil {
		*e.Backfill = cloneTranscript(*e.Backfill)
	}
	e.Diagnostic = ptrCopy(e.Diagnostic)
	if e.Diagnostic != nil {
		*e.Diagnostic = cloneDiagnostic(*e.Diagnostic)
	}
	e.Terminal = ptrCopy(e.Terminal)
	return e
}
