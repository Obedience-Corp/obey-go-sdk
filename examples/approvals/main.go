// Inspect scoped approvals, optionally observe them, or send one explicit decision.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/Obedience-Corp/obey-go-sdk/pkg/obey"
)

func main() {
	if err := run(); err != nil {
		var typed *obey.Error
		if errors.As(err, &typed) {
			fmt.Fprintf(os.Stderr, "code=%s call=%s decision_indeterminate=%v raw=%s\n", typed.Code, typed.CallID, typed.DecisionIndeterminate, typed.RawRecord)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	camp := flag.String("camp", "", "exact camp ID")
	id := flag.String("session", "", "exact session ID")
	call := flag.String("call", "", "one explicit call ID")
	decision := flag.String("decision", "", "approve or deny (required with --call)")
	reason := flag.String("reason", "", "explicit decision reason")
	watch := flag.Bool("watch", false, "observe pending/live approvals without deciding")
	flag.Parse()
	if *camp == "" || *id == "" {
		return errors.New("--camp and --session required")
	}
	if (*call == "") != (*decision == "") || (*decision != "" && *decision != "approve" && *decision != "deny") || (*watch && *call != "") {
		return errors.New("supply --call with --decision approve|deny, or inspect/watch without a decision")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	client, err := obey.NewClientFromPath()
	if err != nil {
		return err
	}
	defer client.Close()
	session, err := client.OpenSession(ctx, obey.SessionRef{CampID: *camp, SessionID: *id})
	if err != nil {
		return err
	}
	if *call != "" {
		return session.RespondApproval(ctx, obey.ApprovalDecision{CallID: *call, Approve: *decision == "approve", Reason: *reason})
	}
	// Snapshot/live repetitions share exact (camp, session, call) identity. Keep
	// observations separate from decision policy; a plan-exit notice is not Approval.
	type approvalKey struct {
		session obey.SessionRef
		call    string
	}
	seen := map[approvalKey]bool{}
	show := func(approval obey.Approval) {
		key := approvalKey{approval.Session, approval.CallID}
		if !seen[key] {
			seen[key] = true
			fmt.Printf("camp=%s session=%s call=%s tool=%s requested=%v expires=%v raw=%s\n", approval.Session.CampID, approval.Session.SessionID, approval.CallID, approval.ToolName, approval.RequestedAt, approval.ExpiresAt, approval.Raw)
		}
	}
	pending, err := session.PendingApprovals(ctx)
	if err != nil {
		return err
	}
	for _, approval := range pending {
		show(approval)
	}
	if !*watch {
		return nil
	}
	stream, err := session.Attach(ctx, obey.AttachOptions{NoBackfill: true})
	if err != nil {
		return err
	}
	defer stream.Close()
	for stream.Next() {
		event := stream.Event()
		if event.Marker != nil {
			for _, approval := range event.Marker.PendingApprovals {
				show(approval)
			}
		}
		if event.Approval != nil {
			show(*event.Approval)
		}
	}
	if errors.Is(stream.Err(), context.Canceled) {
		return nil
	}
	return stream.Err()
}
