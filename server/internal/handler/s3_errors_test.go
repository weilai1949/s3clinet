package handler

import (
	"errors"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

type fakeAPIError struct {
	code string
}

func (e fakeAPIError) Error() string                 { return e.code }
func (e fakeAPIError) ErrorCode() string             { return e.code }
func (e fakeAPIError) ErrorMessage() string          { return e.code }
func (e fakeAPIError) ErrorFault() smithy.ErrorFault { return smithy.FaultUnknown }

func TestS3UserMessage(t *testing.T) {
	if got := s3UserMessage(nil); got != "" {
		t.Fatalf("nil = %q", got)
	}
	if got := s3UserMessage(&types.NoSuchKey{}); got != "object not found" {
		t.Fatalf("NoSuchKey = %q", got)
	}
	if got := s3UserMessage(fakeAPIError{code: "AccessDenied"}); got != "access denied" {
		t.Fatalf("AccessDenied = %q", got)
	}
	if got := s3UserMessage(fmt.Errorf("object x exceeds 5GB single-put limit")); got != "object exceeds 5GB single-put limit; use multipart upload" {
		t.Fatalf("5GB = %q", got)
	}
	if got := s3UserMessage(errors.New("something weird")); got != "storage operation failed" {
		t.Fatalf("unknown = %q", got)
	}
}

func TestBatchItemError(t *testing.T) {
	got := batchItemError("a.txt", fakeAPIError{code: "AccessDenied"})
	want := "failed at a.txt: access denied"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
