package s3wrap

import (
	"errors"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

type fakeAPIError struct{ code string }

func (e fakeAPIError) Error() string                 { return e.code }
func (e fakeAPIError) ErrorCode() string             { return e.code }
func (e fakeAPIError) ErrorMessage() string          { return e.code }
func (e fakeAPIError) ErrorFault() smithy.ErrorFault { return smithy.FaultUnknown }

func TestUserMessageAndHTTPStatus(t *testing.T) {
	if UserMessage(nil) != "" {
		t.Fatal("nil")
	}
	if UserMessage(&types.NoSuchKey{}) != "object not found" || HTTPStatus(&types.NoSuchKey{}) != 404 {
		t.Fatal("NoSuchKey")
	}
	if UserMessage(fakeAPIError{code: "AccessDenied"}) != "access denied" || HTTPStatus(fakeAPIError{code: "AccessDenied"}) != 403 {
		t.Fatal("AccessDenied")
	}
	if !IsEntityTooLarge(fakeAPIError{code: "EntityTooLarge"}) {
		t.Fatal("EntityTooLarge")
	}
	if UserMessage(fmt.Errorf("object x exceeds 5GB")) != "object exceeds 5GB single-put limit; use multipart upload" {
		t.Fatal("5GB")
	}
	if UserMessage(errors.New("weird")) != "storage operation failed" || HTTPStatus(errors.New("weird")) != 500 {
		t.Fatal("unknown")
	}
}
