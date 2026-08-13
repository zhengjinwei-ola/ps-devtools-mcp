package testlogs

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestUnavailableReturnsActionableError(t *testing.T) {
	_, err := (Unavailable{}).Search(context.Background(), SearchInput{})
	if err == nil {
		t.Fatal("expected unavailable error")
	}
}

type fakeLogService struct {
	listCalls  atomic.Int32
	closeCalls atomic.Int32
}

func (f *fakeLogService) ListSources(context.Context, ListSourcesInput) (ListSourcesOutput, error) {
	f.listCalls.Add(1)
	return ListSourcesOutput{Sources: []SourceInfo{{Name: "activity"}}}, nil
}
func (*fakeLogService) Search(context.Context, SearchInput) (SearchOutput, error) {
	return SearchOutput{}, nil
}
func (*fakeLogService) Trace(context.Context, TraceInput) (TraceOutput, error) {
	return TraceOutput{}, nil
}
func (*fakeLogService) ListRuntimeSources(context.Context, ListRuntimeSourcesInput) (ListRuntimeSourcesOutput, error) {
	return ListRuntimeSourcesOutput{}, nil
}
func (*fakeLogService) GetRuntime(context.Context, GetRuntimeInput) (GetRuntimeOutput, error) {
	return GetRuntimeOutput{}, nil
}
func (f *fakeLogService) Close() error {
	f.closeCalls.Add(1)
	return nil
}

func TestLazyDefersAndReusesConnection(t *testing.T) {
	service := &fakeLogService{}
	var connectCalls atomic.Int32
	lazy := newLazy(func(context.Context) (closeableLogService, error) {
		connectCalls.Add(1)
		return service, nil
	})
	if connectCalls.Load() != 0 {
		t.Fatal("constructor connected eagerly")
	}

	const callers = 8
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := lazy.ListSources(context.Background(), ListSourcesInput{}); err != nil {
				t.Errorf("ListSources: %v", err)
			}
		}()
	}
	wg.Wait()
	if got := connectCalls.Load(); got != 1 {
		t.Fatalf("connect calls = %d, want 1", got)
	}
	if got := service.listCalls.Load(); got != callers {
		t.Fatalf("list calls = %d, want %d", got, callers)
	}
}

func TestLazyRetriesConnectionAfterFailure(t *testing.T) {
	service := &fakeLogService{}
	var connectCalls atomic.Int32
	lazy := newLazy(func(context.Context) (closeableLogService, error) {
		if connectCalls.Add(1) == 1 {
			return nil, errors.New("temporary failure")
		}
		return service, nil
	})
	if _, err := lazy.ListSources(context.Background(), ListSourcesInput{}); err == nil {
		t.Fatal("expected first connection to fail")
	}
	if _, err := lazy.ListSources(context.Background(), ListSourcesInput{}); err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	if got := connectCalls.Load(); got != 2 {
		t.Fatalf("connect calls = %d, want 2", got)
	}
}

func TestLazyCloseWithoutUseDoesNotConnect(t *testing.T) {
	var connectCalls atomic.Int32
	lazy := newLazy(func(context.Context) (closeableLogService, error) {
		connectCalls.Add(1)
		return &fakeLogService{}, nil
	})
	if err := lazy.Close(); err != nil {
		t.Fatal(err)
	}
	if connectCalls.Load() != 0 {
		t.Fatal("Close connected an unused backend")
	}
	if _, err := lazy.ListSources(context.Background(), ListSourcesInput{}); err == nil {
		t.Fatal("expected call after Close to fail")
	}
}
