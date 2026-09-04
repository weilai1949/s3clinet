package s3wrap

import (
	"errors"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// TestUserMessageCoversAllBranches 错误→用户文案映射全分支（表驱动）。
func TestUserMessageCoversAllBranches(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"nil error", nil, ""},
		{"5GB single put limit", errors.New("put failed: object exceeds 5GB limit"), "object exceeds 5GB single-put limit; use multipart upload"},
		{"copy source delete failure", errors.New("copied ok, failed to delete source object"), "copied but failed to delete source"},
		{"NoSuchBucket typed", &types.NoSuchBucket{}, "bucket not found"},
		{"NoSuchKey typed", &types.NoSuchKey{}, "object not found"},
		{"NotFound code", fakeAPIError{code: "NotFound"}, "object not found"},
		{"NoSuchVersion code", fakeAPIError{code: "NoSuchVersion"}, "object not found"},
		{"AccessDenied", fakeAPIError{code: "AccessDenied"}, "access denied"},
		{"InvalidAccessKeyId", fakeAPIError{code: "InvalidAccessKeyId"}, "invalid credentials"},
		{"SignatureDoesNotMatch", fakeAPIError{code: "SignatureDoesNotMatch"}, "invalid credentials"},
		{"BucketNotEmpty", fakeAPIError{code: "BucketNotEmpty"}, "bucket not empty"},
		{"InvalidRequest", fakeAPIError{code: "InvalidRequest"}, "invalid request"},
		{"InvalidArgument", fakeAPIError{code: "InvalidArgument"}, "invalid request"},
		{"MalformedPolicy", fakeAPIError{code: "MalformedPolicy"}, "invalid request"},
		{"MalformedXML", fakeAPIError{code: "MalformedXML"}, "invalid request"},
		{"InvalidStorageClass", fakeAPIError{code: "InvalidStorageClass"}, "invalid request"},
		{"EntityTooLarge", fakeAPIError{code: "EntityTooLarge"}, "entity too large"},
		{"SlowDown", fakeAPIError{code: "SlowDown"}, "storage temporarily unavailable"},
		{"ServiceUnavailable", fakeAPIError{code: "ServiceUnavailable"}, "storage temporarily unavailable"},
		{"RequestTimeout", fakeAPIError{code: "RequestTimeout"}, "storage temporarily unavailable"},
		{"NoSuchUpload", fakeAPIError{code: "NoSuchUpload"}, "multipart upload not found"},
		{"unknown error", errors.New("connection refused"), "storage operation failed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := UserMessage(c.err); got != c.want {
				t.Fatalf("UserMessage(%v) = %q, want %q", c.err, got, c.want)
			}
		})
	}
}

// TestHTTPStatusCoversAllBranches 错误→HTTP 状态映射全分支（表驱动）。
func TestHTTPStatusCoversAllBranches(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil error", nil, 500},
		{"NoSuchKey typed", &types.NoSuchKey{}, 404},
		{"NotFound code", fakeAPIError{code: "NotFound"}, 404},
		{"AccessDenied", fakeAPIError{code: "AccessDenied"}, 403},
		{"InvalidAccessKeyId", fakeAPIError{code: "InvalidAccessKeyId"}, 403},
		{"SignatureDoesNotMatch", fakeAPIError{code: "SignatureDoesNotMatch"}, 403},
		{"InvalidRequest", fakeAPIError{code: "InvalidRequest"}, 400},
		{"InvalidArgument", fakeAPIError{code: "InvalidArgument"}, 400},
		{"MalformedPolicy", fakeAPIError{code: "MalformedPolicy"}, 400},
		{"MalformedXML", fakeAPIError{code: "MalformedXML"}, 400},
		{"EntityTooLarge", fakeAPIError{code: "EntityTooLarge"}, 400},
		{"InvalidStorageClass", fakeAPIError{code: "InvalidStorageClass"}, 400},
		{"BucketNotEmpty", fakeAPIError{code: "BucketNotEmpty"}, 409},
		{"InvalidRange", fakeAPIError{code: "InvalidRange"}, 416},
		{"SlowDown", fakeAPIError{code: "SlowDown"}, 503},
		{"ServiceUnavailable", fakeAPIError{code: "ServiceUnavailable"}, 503},
		{"NoSuchUpload unmapped", fakeAPIError{code: "NoSuchUpload"}, 500},
		{"unknown error", errors.New("boom"), 500},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := HTTPStatus(c.err); got != c.want {
				t.Fatalf("HTTPStatus(%v) = %d, want %d", c.err, got, c.want)
			}
		})
	}
}

// TestErrorCodeExtraction 提取 smithy 错误码：直接错误、包装错误、非 API 错误。
func TestErrorCodeExtraction(t *testing.T) {
	if got := ErrorCode(fakeAPIError{code: "AccessDenied"}); got != "AccessDenied" {
		t.Fatalf("ErrorCode = %q", got)
	}
	wrapped := fmt.Errorf("api error AccessDenied: wrapped: %w", fakeAPIError{code: "NoSuchBucket"})
	if got := ErrorCode(wrapped); got != "NoSuchBucket" {
		t.Fatalf("ErrorCode(wrapped) = %q, want NoSuchBucket", got)
	}
	if got := ErrorCode(errors.New("plain")); got != "" {
		t.Fatalf("ErrorCode(plain) = %q, want empty", got)
	}
	if ErrorCode(nil) != "" {
		t.Fatal("ErrorCode(nil) should be empty")
	}
}

// TestHasErrorCode 多候选错误码匹配。
func TestHasErrorCode(t *testing.T) {
	err := fakeAPIError{code: "NoSuchTagSet"}
	if !HasErrorCode(err, "NoSuchTagSet", "NoSuchTagSetError") {
		t.Fatal("should match one of the codes")
	}
	if HasErrorCode(err, "AccessDenied") {
		t.Fatal("should not match unrelated code")
	}
	if HasErrorCode(errors.New("plain"), "AccessDenied") {
		t.Fatal("non-API error has no code")
	}
	if HasErrorCode(nil, "AccessDenied") {
		t.Fatal("nil has no code")
	}
}

// TestIsNotFoundCoversAllForms 对象/桶不存在：typed 错误、错误码、普通错误、nil。
func TestIsNotFoundCoversAllForms(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"types.NotFound", &types.NotFound{}, true},
		{"types.NoSuchKey", &types.NoSuchKey{}, true},
		{"types.NoSuchBucket", &types.NoSuchBucket{}, true},
		{"code NotFound", fakeAPIError{code: "NotFound"}, true},
		{"code NoSuchKey", fakeAPIError{code: "NoSuchKey"}, true},
		{"code NoSuchBucket", fakeAPIError{code: "NoSuchBucket"}, true},
		{"code NoSuchVersion", fakeAPIError{code: "NoSuchVersion"}, true},
		{"wrapped NoSuchKey", fmt.Errorf("get: %w", &types.NoSuchKey{}), true},
		{"unrelated code", fakeAPIError{code: "AccessDenied"}, false},
		{"plain error", errors.New("boom"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsNotFound(c.err); got != c.want {
				t.Fatalf("IsNotFound(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}

// TestIsAPIError 可识别的 S3 API 错误 vs 普通错误。
func TestIsAPIError(t *testing.T) {
	if !IsAPIError(fakeAPIError{code: "AccessDenied"}) {
		t.Fatal("API error should be recognized")
	}
	if IsAPIError(errors.New("dial tcp: refused")) {
		t.Fatal("plain error is not an API error")
	}
	if IsAPIError(nil) {
		t.Fatal("nil is not an API error")
	}
}

// TestIsEntityTooLargeForms 错误码 / 文案 / 包装三种形态都能识别对象过大。
func TestIsEntityTooLargeForms(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"code", fakeAPIError{code: "EntityTooLarge"}, true},
		{"user message path", fakeAPIError{code: "EntityTooLarge"}, true},
		{"message contains code", errors.New("validation: EntityTooLarge for part 1"), true},
		{"message lower case", errors.New("part is entity too large"), true},
		{"unrelated", errors.New("boom"), false},
		{"unrelated api error", fakeAPIError{code: "AccessDenied"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsEntityTooLarge(c.err); got != c.want {
				t.Fatalf("IsEntityTooLarge(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}

// 编译期确认 smithy 依赖仍被引用（fakeAPIError 定义在 errors_test.go）。
var _ smithy.APIError = fakeAPIError{}
