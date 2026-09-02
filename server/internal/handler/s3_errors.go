package handler

import (
	"github.com/weilai1949/s3clinet/server/internal/s3wrap"
)

// s3UserMessage 委托防腐层。
func s3UserMessage(err error) string { return s3wrap.UserMessage(err) }

// s3HTTPStatus 委托防腐层。
func s3HTTPStatus(err error) int { return s3wrap.HTTPStatus(err) }

// batchItemError 批量操作中单 key 失败的用户可见摘要。
func batchItemError(key string, err error) string {
	return "failed at " + key + ": " + s3UserMessage(err)
}
