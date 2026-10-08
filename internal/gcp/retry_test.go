package gcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/logging"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

// fastRetry shrinks backoff delays and captures retry notices for a test.
func fastRetry(t *testing.T) *[]string {
	t.Helper()
	prevA, prevI, prevM, prevN := retryAttempts, retryInitialDelay, retryMaxDelay, retryNotify
	t.Cleanup(func() {
		retryAttempts, retryInitialDelay, retryMaxDelay, retryNotify = prevA, prevI, prevM, prevN
	})
	var notices []string
	retryAttempts = 4
	retryInitialDelay = time.Millisecond
	retryMaxDelay = 2 * time.Millisecond
	retryNotify = func(msg string) { notices = append(notices, msg) }
	return &notices
}

var quotaErr = status.Error(codes.ResourceExhausted,
	"Quota exceeded for quota metric 'Read requests' and limit 'Read requests per minute'")

func makeEntries(n int) []*logging.Entry {
	es := make([]*logging.Entry, n)
	for i := range es {
		es[i] = &logging.Entry{InsertID: fmt.Sprintf("e%d", i)}
	}
	return es
}

func TestIsQuotaError(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{quotaErr, true},
		{fmt.Errorf("wrapped: %w", quotaErr), true},
		{errors.New("rpc error: code = ResourceExhausted desc = Quota exceeded"), true},
		{errors.New("googleapi: Error 429: RATE_LIMIT_EXCEEDED"), true},
		{status.Error(codes.PermissionDenied, "denied"), false},
		{errors.New("network unreachable"), false},
	}
	for _, tt := range tests {
		if got := isQuotaError(tt.err); got != tt.want {
			t.Errorf("isQuotaError(%v) = %v, want %v", tt.err, got, tt.want)
		}
	}
}

func TestWithRetry_SucceedsAfterQuotaErrors(t *testing.T) {
	notices := fastRetry(t)
	calls := 0
	err := withRetry(context.Background(), func() error {
		calls++
		if calls < 3 {
			return quotaErr
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
	if len(*notices) != 2 || !strings.Contains((*notices)[0], "quota exceeded") {
		t.Errorf("expected 2 retry notices, got %q", *notices)
	}
}

func TestWithRetry_GivesUpAfterMaxAttempts(t *testing.T) {
	fastRetry(t)
	calls := 0
	err := withRetry(context.Background(), func() error {
		calls++
		return quotaErr
	})
	if !isQuotaError(err) {
		t.Fatalf("expected quota error after giving up, got %v", err)
	}
	if calls != retryAttempts {
		t.Errorf("calls = %d, want %d", calls, retryAttempts)
	}
}

func TestWithRetry_NonQuotaErrorNotRetried(t *testing.T) {
	notices := fastRetry(t)
	calls := 0
	want := status.Error(codes.PermissionDenied, "denied")
	err := withRetry(context.Background(), func() error {
		calls++
		return want
	})
	if err != want || calls != 1 || len(*notices) != 0 {
		t.Errorf("err=%v calls=%d notices=%q; want no retry", err, calls, *notices)
	}
}

func TestWithRetry_RespectsContext(t *testing.T) {
	fastRetry(t)
	retryInitialDelay = time.Hour
	retryMaxDelay = time.Hour
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := withRetry(ctx, func() error { return quotaErr })
	if err == nil || !errors.Is(err, quotaErr) {
		t.Fatalf("expected wrapped quota error, got %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("withRetry did not stop on context cancellation")
	}
}

func TestCollectEntries_PageSizeMatchesLimit(t *testing.T) {
	fastRetry(t)
	var sizes []int
	fetch := func(_ context.Context, token string, size int) ([]*logging.Entry, string, error) {
		sizes = append(sizes, size)
		return makeEntries(size), "more", nil
	}
	got, err := collectEntries(context.Background(), fetch, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 5 {
		t.Errorf("got %d entries, want 5", len(got))
	}
	if len(sizes) != 1 || sizes[0] != 5 {
		t.Errorf("page sizes = %v, want a single page of 5", sizes)
	}
}

func TestCollectEntries_LargeLimitPagesAtMax(t *testing.T) {
	fastRetry(t)
	var sizes []int
	fetch := func(_ context.Context, token string, size int) ([]*logging.Entry, string, error) {
		sizes = append(sizes, size)
		return makeEntries(size), "more", nil
	}
	got, err := collectEntries(context.Background(), fetch, 2500)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2500 {
		t.Errorf("got %d entries, want 2500", len(got))
	}
	want := []int{1000, 1000, 500}
	if fmt.Sprint(sizes) != fmt.Sprint(want) {
		t.Errorf("page sizes = %v, want %v", sizes, want)
	}
}

func TestCollectEntries_StopsWhenExhausted(t *testing.T) {
	fastRetry(t)
	calls := 0
	fetch := func(_ context.Context, token string, size int) ([]*logging.Entry, string, error) {
		calls++
		return makeEntries(3), "", nil
	}
	got, err := collectEntries(context.Background(), fetch, 100)
	if err != nil || len(got) != 3 || calls != 1 {
		t.Errorf("got %d entries, err=%v, calls=%d; want 3 entries in 1 call", len(got), err, calls)
	}
}

func TestCollectEntries_RetriesPageFromSameToken(t *testing.T) {
	notices := fastRetry(t)
	var tokens []string
	fetch := func(_ context.Context, token string, size int) ([]*logging.Entry, string, error) {
		tokens = append(tokens, token)
		switch len(tokens) {
		case 1:
			return makeEntries(2), "t1", nil
		case 2:
			return nil, "", quotaErr
		default:
			return makeEntries(2), "", nil
		}
	}
	got, err := collectEntries(context.Background(), fetch, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 4 {
		t.Errorf("got %d entries, want 4 (no duplicates or gaps)", len(got))
	}
	if fmt.Sprint(tokens) != fmt.Sprint([]string{"", "t1", "t1"}) {
		t.Errorf("tokens = %q, want retry from same token", tokens)
	}
	if len(*notices) != 1 {
		t.Errorf("expected 1 retry notice, got %q", *notices)
	}
}

func TestEntryToMap_StructPayloadIsJsonPayload(t *testing.T) {
	s, err := structpb.NewStruct(map[string]interface{}{
		"reason":         "FailedPreStopHook",
		"involvedObject": map[string]interface{}{"name": "pod-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	m := entryToMap(&logging.Entry{Timestamp: time.Now(), Payload: s})
	if _, ok := m["protoPayload"]; ok {
		t.Errorf("structpb payload must not be labelled protoPayload: %v", m)
	}
	jp, ok := m["jsonPayload"].(map[string]interface{})
	if !ok || jp["reason"] != "FailedPreStopHook" {
		t.Fatalf("jsonPayload missing or wrong: %v", m["jsonPayload"])
	}
	entry := ExtractFieldsWithPaths(m, []string{"pod"}, map[string]string{"pod": "jsonPayload.involvedObject.name"})
	if entry.Fields["pod"] != "pod-1" {
		t.Errorf("pod: got %q", entry.Fields["pod"])
	}
}
