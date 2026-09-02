package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type deadlineStub struct {
	http.ResponseWriter
	called bool
}

func (d *deadlineStub) SetWriteDeadline(t time.Time) error {
	d.called = true
	return nil
}

// TestStatusRecorderUnwrap 确保日志包装器可被 ResponseController 穿透到底层连接。
func TestStatusRecorderUnwrap(t *testing.T) {
	inner := &deadlineStub{ResponseWriter: httptest.NewRecorder()}
	rec := &statusRecorder{ResponseWriter: inner, status: 200}
	if got := rec.Unwrap(); got != inner {
		t.Fatalf("Unwrap() = %T, want underlying ResponseWriter", got)
	}
	rc := http.NewResponseController(rec)
	if err := rc.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetWriteDeadline via statusRecorder: %v (Unwrap missing?)", err)
	}
	if !inner.called {
		t.Fatal("SetWriteDeadline was not forwarded to underlying writer")
	}
}
