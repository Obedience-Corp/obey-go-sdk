// Read-only history pagination and explicitly requested attach/reconnect.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/Obedience-Corp/obey-go-sdk/pkg/obey"
	"os"
	"os/signal"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	camp := flag.String("camp", "", "exact camp ID")
	sessionID := flag.String("session", "", "exact session ID")
	mode := flag.String("mode", "history", "history or attach")
	from := flag.Uint64("from-seq", 0, "explicit cursor in this session's durable sequence scope")
	cursor := flag.String("cursor", "", "opaque history cursor")
	flag.Parse()
	if *camp == "" || *sessionID == "" {
		return errors.New("--camp and --session IDs are required")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	client, err := obey.NewClientFromPath()
	if err != nil {
		return err
	}
	defer client.Close()
	session, err := client.OpenSession(ctx, obey.SessionRef{CampID: *camp, SessionID: *sessionID})
	if err != nil {
		return err
	}
	switch *mode {
	case "history":
		next := *cursor
		for {
			page, err := session.Transcript(ctx, obey.TranscriptOptions{Cursor: next, PageSize: 100})
			if err != nil {
				return err
			}
			fmt.Printf("%s\n", page.Raw)
			if !page.HasMore {
				return nil
			}
			if page.NextCursor == "" || page.NextCursor == next {
				return errors.New("history cursor did not advance")
			}
			next = page.NextCursor
		}
	case "attach":
		stream, err := session.Attach(ctx, obey.AttachOptions{FromSequence: *from, BackfillLimit: 500})
		if err != nil {
			return err
		}
		defer stream.Close()
		cursorSession := session.Ref()
		last := *from
		for stream.Next() {
			event := stream.Event()
			fmt.Printf("kind=%s requested=%s observed=%s raw=%s\n", event.Kind, event.RequestedSessionID, event.Session.SessionID, event.RawPayload)
			if event.Session.SessionID != "" && event.Session != cursorSession {
				cursorSession = event.Session
				last = 0
			}
			if event.DurableSequence != nil && *event.DurableSequence > 0 {
				last = *event.DurableSequence
			}
		}
		// Reconnect only when the caller explicitly invokes this program again. Save
		// sequence together with its actual session ID; a marker does not prove replay.
		fmt.Printf("next explicit reconnect: session=%s from-seq=%d\n", cursorSession.SessionID, last)
		if errors.Is(stream.Err(), context.Canceled) {
			return nil
		}
		return stream.Err()
	default:
		return errors.New("--mode must be history or attach")
	}
}
