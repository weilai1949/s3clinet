package s3wrap

import "github.com/aws/aws-sdk-go-v2/aws"

func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	return aws.String(s)
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefInt32(i *int32) int32 {
	if i == nil {
		return 0
	}
	return *i
}

func boolOrFalse(b *bool) bool {
	if b == nil {
		return false
	}
	return *b
}
