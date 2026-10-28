package execclient

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestRecords(t *testing.T) {
	limits, _ := (Limits{MaxRecordBytes: 8, MaxResultBytes: 16}).normalized()
	var got []string
	if err := readRecords(strings.NewReader("one\ntwo"), limits, func(b []byte) error { got = append(got, string(b)); return nil }); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"one", "two"}) {
		t.Fatal(got)
	}
	for _, s := range []string{"123456789"} {
		if err := readRecords(strings.NewReader(s), limits, func([]byte) error { return nil }); !errors.Is(err, ErrOutputLimit) {
			t.Fatal(err)
		}
	}
	stop := errors.New("consumer parse error")
	if err := readRecords(strings.NewReader("{}\n"), limits, func([]byte) error { return stop }); !errors.Is(err, stop) {
		t.Fatal(err)
	}
}
func TestLargeFragmentedRecord(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 100000)
	l, _ := (Limits{}).normalized()
	var got []byte
	if err := readRecords(bytes.NewReader(data), l, func(b []byte) error { got = b; return nil }); err != nil || !bytes.Equal(got, data) {
		t.Fatal(err)
	}
}
func TestTailBuffer(t *testing.T) {
	b := tailBuffer{limit: 4}
	for _, s := range []string{"ab", "cde", "fghijk"} {
		_, _ = b.Write([]byte(s))
	}
	if b.String() != "hijk" {
		t.Fatal(b.String())
	}
}
func TestInvalidConfiguration(t *testing.T) {
	if _, err := New("relative", Limits{}); err == nil {
		t.Fatal("relative binary accepted")
	}
	if _, err := New("/missing", Limits{MaxRecordBytes: -1}); err == nil {
		t.Fatal("negative limit accepted")
	}
}

func TestStreamDoesNotAccumulateResultHistory(t *testing.T) {
	l, _ := (Limits{MaxRecordBytes: 8, MaxResultBytes: 8}).normalized()
	count := 0
	if err := readRecords(strings.NewReader(strings.Repeat("{}\n", 10000)), l, func([]byte) error { count++; return nil }); err != nil || count != 10000 {
		t.Fatalf("records=%d err=%v", count, err)
	}
}

func TestCleanExitAlwaysEOF(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(nil)
	records := make(chan []byte)
	close(records)
	done := make(chan struct{})
	close(done)
	p := &Process{ctx: ctx, records: records, done: done}
	for i := 0; i < 10000; i++ {
		if _, err := p.Next(context.Background()); err != io.EOF {
			t.Fatalf("iteration%d: %v", i, err)
		}
	}
}

func TestShutdownBudgetDefaultsAndNegative(t *testing.T) {
	limits, err := (Limits{}).normalized()
	if err != nil || limits.MaxShutdownBytes != DefaultBytes {
		t.Fatalf("defaults=%+v %v", limits, err)
	}
	if _, err := (Limits{MaxShutdownBytes: -1}).normalized(); err == nil {
		t.Fatal("negative shutdown budget accepted")
	}
}
