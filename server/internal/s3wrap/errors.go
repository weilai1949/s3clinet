package s3wrap

import (
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// UserMessage 将 S3/SDK 错误映射为面向用户的短消息（防腐层出口）。
func UserMessage(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if strings.Contains(msg, "exceeds 5GB") {
		return "object exceeds 5GB single-put limit; use multipart upload"
	}
	if strings.Contains(msg, "failed to delete source") {
		return "copied but failed to delete source"
	}
	if IsNotFound(err) {
		if HasErrorCode(err, "NoSuchBucket") {
			return "bucket not found"
		}
		return "object not found"
	}
	switch ErrorCode(err) {
	case "AccessDenied":
		return "access denied"
	case "InvalidAccessKeyId", "SignatureDoesNotMatch":
		return "invalid credentials"
	case "BucketNotEmpty":
		return "bucket not empty"
	case "InvalidRequest", "InvalidArgument", "MalformedPolicy", "MalformedXML", "InvalidStorageClass":
		return "invalid request"
	case "EntityTooLarge":
		return "entity too large"
	case "SlowDown", "ServiceUnavailable", "RequestTimeout":
		return "storage temporarily unavailable"
	case "NoSuchUpload":
		return "multipart upload not found"
	}
	return "storage operation failed"
}

// HTTPStatus 将常见 S3 错误映射为稳定 HTTP 状态。
func HTTPStatus(err error) int {
	if err == nil {
		return 500
	}
	if IsNotFound(err) {
		return 404
	}
	switch ErrorCode(err) {
	case "AccessDenied", "InvalidAccessKeyId", "SignatureDoesNotMatch":
		return 403
	case "InvalidRequest", "InvalidArgument", "MalformedPolicy", "MalformedXML", "EntityTooLarge", "InvalidStorageClass":
		return 400
	case "BucketNotEmpty":
		return 409
	case "InvalidRange":
		return 416
	case "SlowDown", "ServiceUnavailable":
		return 503
	}
	return 500
}

// ErrorCode 提取 smithy API 错误码；非 API 错误返回空串。
func ErrorCode(err error) string {
	var ae smithy.APIError
	if errors.As(err, &ae) {
		return ae.ErrorCode()
	}
	return ""
}

// HasErrorCode 判断是否为给定错误码之一。
func HasErrorCode(err error, codes ...string) bool {
	code := ErrorCode(err)
	if code == "" {
		return false
	}
	for _, c := range codes {
		if code == c {
			return true
		}
	}
	return false
}

// IsNotFound 对象/桶不存在。
func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	var nf *types.NotFound
	var nsk *types.NoSuchKey
	var ncb *types.NoSuchBucket
	if errors.As(err, &nf) || errors.As(err, &nsk) || errors.As(err, &ncb) {
		return true
	}
	return HasErrorCode(err, "NoSuchBucket", "NotFound", "NoSuchKey", "NoSuchVersion")
}

// IsAPIError 是否为可识别的 S3 API 错误。
func IsAPIError(err error) bool { return ErrorCode(err) != "" }

// IsEntityTooLarge 判断是否为对象过大错误。
func IsEntityTooLarge(err error) bool {
	if err == nil {
		return false
	}
	if HasErrorCode(err, "EntityTooLarge") || UserMessage(err) == "entity too large" {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "EntityTooLarge") || strings.Contains(msg, "entity too large")
}
