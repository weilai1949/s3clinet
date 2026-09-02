package handler

import "github.com/weilai1949/s3clinet/server/internal/service"

type migrateRequest struct {
	SourceAccountID string   `json:"sourceAccountId"`
	SourceBucket    string   `json:"sourceBucket"`
	SourceKeys      []string `json:"sourceKeys"`
	TargetAccountID string   `json:"targetAccountId"`
	TargetBucket    string   `json:"targetBucket"`
	TargetPrefix    string   `json:"targetPrefix"`
}

type migrateResult struct {
	Migrated  int
	Failed    int
	LastError string
	FailKeys  []string
}

func migrateResultJSON(out migrateResult) map[string]any {
	resp := map[string]any{"migrated": out.Migrated, "failed": out.Failed}
	if out.LastError != "" {
		resp["lastError"] = out.LastError
	}
	if len(out.FailKeys) > 0 {
		resp["failedKeys"] = out.FailKeys
	}
	return resp
}

func migrateBatchJSON(out service.BatchResult) map[string]any {
	return migrateResultJSON(migrateResult{
		Migrated: out.OK, Failed: out.Failed, LastError: out.LastError, FailKeys: out.FailKeys,
	})
}
