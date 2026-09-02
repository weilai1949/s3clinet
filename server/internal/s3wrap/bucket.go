package s3wrap

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// HeadBucket 用于连通性测试与确认桶存在。
func (c *Client) HeadBucket(ctx context.Context, bucket string) error {
	_, err := c.s3.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(bucket)})
	return err
}

// ListBuckets 列出账号下所有桶。
func (c *Client) ListBuckets(ctx context.Context) (*s3.ListBucketsOutput, error) {
	return c.s3.ListBuckets(ctx, &s3.ListBucketsInput{})
}

// CreateBucket 创建桶；region 非 us-east-1 时附带 LocationConstraint（OSS/COS/TOS 需要）。
func (c *Client) CreateBucket(ctx context.Context, name, region, acl string) error {
	in := &s3.CreateBucketInput{Bucket: aws.String(name)}
	if acl != "" {
		in.ACL = types.BucketCannedACL(acl)
	}
	if region != "" && region != "us-east-1" {
		in.CreateBucketConfiguration = &types.CreateBucketConfiguration{
			LocationConstraint: types.BucketLocationConstraint(region),
		}
	}
	_, err := c.s3.CreateBucket(ctx, in)
	return err
}

// DeleteBucket 删除桶（桶内须为空，否则返回 BucketNotEmpty）。
func (c *Client) DeleteBucket(ctx context.Context, name string) error {
	_, err := c.s3.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: aws.String(name)})
	return err
}

// LifecycleRuleSpec 生命周期过期规则（简化版：前缀 + 过期天数）。
type LifecycleRuleSpec struct {
	ID     string
	Prefix string
	Days   int32
}

// GetLifecycle 读取桶的生命周期规则。
func (c *Client) GetLifecycle(ctx context.Context, bucket string) ([]LifecycleRuleSpec, error) {
	out, err := c.s3.GetBucketLifecycleConfiguration(ctx, &s3.GetBucketLifecycleConfigurationInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		return nil, err
	}
	specs := make([]LifecycleRuleSpec, 0, len(out.Rules))
	for _, r := range out.Rules {
		d := int32(0)
		if r.Expiration != nil && r.Expiration.Days != nil {
			d = *r.Expiration.Days
		}
		specs = append(specs, LifecycleRuleSpec{
			ID:     derefString(r.ID),
			Prefix: derefString(r.Prefix),
			Days:   d,
		})
	}
	return specs, nil
}

// PutLifecycle 覆盖写入桶的生命周期规则（全部规则一次提交）。
func (c *Client) PutLifecycle(ctx context.Context, bucket string, specs []LifecycleRuleSpec) error {
	rules := make([]types.LifecycleRule, 0, len(specs))
	for _, s := range specs {
		rules = append(rules, types.LifecycleRule{
			ID:     aws.String(s.ID),
			Prefix: aws.String(s.Prefix),
			Status: types.ExpirationStatusEnabled,
			Expiration: &types.LifecycleExpiration{
				Days: aws.Int32(s.Days),
			},
		})
	}
	_, err := c.s3.PutBucketLifecycleConfiguration(ctx, &s3.PutBucketLifecycleConfigurationInput{
		Bucket:                 aws.String(bucket),
		LifecycleConfiguration: &types.BucketLifecycleConfiguration{Rules: rules},
	})
	return err
}

// DeleteLifecycle 删除桶的全部生命周期规则。
func (c *Client) DeleteLifecycle(ctx context.Context, bucket string) error {
	_, err := c.s3.DeleteBucketLifecycle(ctx, &s3.DeleteBucketLifecycleInput{
		Bucket: aws.String(bucket),
	})
	return err
}

// GetBucketLocation 返回桶所在区域（LocationConstraint 为空时视为 us-east-1）。
func (c *Client) GetBucketLocation(ctx context.Context, bucket string) (string, error) {
	out, err := c.s3.GetBucketLocation(ctx, &s3.GetBucketLocationInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		return "", err
	}
	region := string(out.LocationConstraint)
	if region == "" {
		region = "us-east-1"
	}
	return region, nil
}

// GetBucketVersioning 返回版本控制状态（""=未配置 / Enabled / Suspended）。
func (c *Client) GetBucketVersioning(ctx context.Context, bucket string) (string, error) {
	out, err := c.s3.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		return "", err
	}
	return string(out.Status), nil
}

// PutBucketVersioning 开启/暂停版本控制（status: Enabled | Suspended）。
func (c *Client) PutBucketVersioning(ctx context.Context, bucket, status string) error {
	_, err := c.s3.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
		Bucket: aws.String(bucket),
		VersioningConfiguration: &types.VersioningConfiguration{
			Status: types.BucketVersioningStatus(status),
		},
	})
	return err
}

// EncryptionConfig 桶的服务端加密配置（SSE）。
type EncryptionConfig struct {
	Algorithm        string
	KMSKeyID         string
	BucketKeyEnabled bool
}

// GetEncryption 读取桶的默认服务端加密。
func (c *Client) GetEncryption(ctx context.Context, bucket string) (*EncryptionConfig, error) {
	out, err := c.s3.GetBucketEncryption(ctx, &s3.GetBucketEncryptionInput{Bucket: aws.String(bucket)})
	if err != nil {
		return nil, err
	}
	if out.ServerSideEncryptionConfiguration == nil || len(out.ServerSideEncryptionConfiguration.Rules) == 0 {
		return nil, errors.New("no encryption rules")
	}
	r := out.ServerSideEncryptionConfiguration.Rules[0]
	cfg := &EncryptionConfig{}
	if r.ApplyServerSideEncryptionByDefault != nil {
		cfg.Algorithm = string(r.ApplyServerSideEncryptionByDefault.SSEAlgorithm)
		cfg.KMSKeyID = derefString(r.ApplyServerSideEncryptionByDefault.KMSMasterKeyID)
	}
	cfg.BucketKeyEnabled = boolOrFalse(r.BucketKeyEnabled)
	return cfg, nil
}

// PutEncryption 设置桶的默认服务端加密。
func (c *Client) PutEncryption(ctx context.Context, bucket string, enc EncryptionConfig) error {
	if enc.Algorithm == "" {
		return errors.New("algorithm is required")
	}
	rule := types.ServerSideEncryptionRule{
		ApplyServerSideEncryptionByDefault: &types.ServerSideEncryptionByDefault{
			SSEAlgorithm: types.ServerSideEncryption(enc.Algorithm),
		},
		BucketKeyEnabled: aws.Bool(enc.BucketKeyEnabled),
	}
	if enc.KMSKeyID != "" {
		rule.ApplyServerSideEncryptionByDefault.KMSMasterKeyID = aws.String(enc.KMSKeyID)
	}
	_, err := c.s3.PutBucketEncryption(ctx, &s3.PutBucketEncryptionInput{
		Bucket: aws.String(bucket),
		ServerSideEncryptionConfiguration: &types.ServerSideEncryptionConfiguration{
			Rules: []types.ServerSideEncryptionRule{rule},
		},
	})
	return err
}

// DeleteEncryption 删除桶的默认服务端加密配置。
func (c *Client) DeleteEncryption(ctx context.Context, bucket string) error {
	_, err := c.s3.DeleteBucketEncryption(ctx, &s3.DeleteBucketEncryptionInput{Bucket: aws.String(bucket)})
	return err
}

// CorsRule CORS 规则（简化为常用字段）。
type CorsRule struct {
	ID             string
	AllowedMethods []string
	AllowedOrigins []string
	AllowedHeaders []string
	ExposeHeaders  []string
	MaxAgeSeconds  int32
}

// GetCors 读取桶 CORS 规则。
func (c *Client) GetCors(ctx context.Context, bucket string) ([]CorsRule, error) {
	out, err := c.s3.GetBucketCors(ctx, &s3.GetBucketCorsInput{Bucket: aws.String(bucket)})
	if err != nil {
		return nil, err
	}
	rules := make([]CorsRule, 0, len(out.CORSRules))
	for _, r := range out.CORSRules {
		rules = append(rules, CorsRule{
			ID:             derefString(r.ID),
			AllowedMethods: r.AllowedMethods,
			AllowedOrigins: r.AllowedOrigins,
			AllowedHeaders: r.AllowedHeaders,
			ExposeHeaders:  r.ExposeHeaders,
			MaxAgeSeconds:  derefInt32(r.MaxAgeSeconds),
		})
	}
	return rules, nil
}

// PutCors 覆盖写入桶 CORS 规则。
func (c *Client) PutCors(ctx context.Context, bucket string, rules []CorsRule) error {
	corsRules := make([]types.CORSRule, 0, len(rules))
	for _, r := range rules {
		corsRules = append(corsRules, types.CORSRule{
			ID:             optionalString(r.ID),
			AllowedMethods: r.AllowedMethods,
			AllowedOrigins: r.AllowedOrigins,
			AllowedHeaders: r.AllowedHeaders,
			ExposeHeaders:  r.ExposeHeaders,
			MaxAgeSeconds:  aws.Int32(r.MaxAgeSeconds),
		})
	}
	_, err := c.s3.PutBucketCors(ctx, &s3.PutBucketCorsInput{
		Bucket:            aws.String(bucket),
		CORSConfiguration: &types.CORSConfiguration{CORSRules: corsRules},
	})
	return err
}

// DeleteCors 删除桶 CORS 规则。
func (c *Client) DeleteCors(ctx context.Context, bucket string) error {
	_, err := c.s3.DeleteBucketCors(ctx, &s3.DeleteBucketCorsInput{Bucket: aws.String(bucket)})
	return err
}

// WebsiteConfig 静态网站托管配置。
type WebsiteConfig struct {
	IndexDocument         string
	ErrorDocument         string
	RedirectAllRequestsTo string
}

// GetWebsite 读取桶的静态网站托管配置。
func (c *Client) GetWebsite(ctx context.Context, bucket string) (*WebsiteConfig, error) {
	out, err := c.s3.GetBucketWebsite(ctx, &s3.GetBucketWebsiteInput{Bucket: aws.String(bucket)})
	if err != nil {
		return nil, err
	}
	wc := &WebsiteConfig{}
	if out.IndexDocument != nil {
		wc.IndexDocument = derefString(out.IndexDocument.Suffix)
	}
	if out.ErrorDocument != nil {
		wc.ErrorDocument = derefString(out.ErrorDocument.Key)
	}
	if out.RedirectAllRequestsTo != nil {
		wc.RedirectAllRequestsTo = derefString(out.RedirectAllRequestsTo.HostName)
	}
	return wc, nil
}

// PutWebsite 写入桶的静态网站托管配置。
func (c *Client) PutWebsite(ctx context.Context, bucket string, wc WebsiteConfig) error {
	in := &s3.PutBucketWebsiteInput{
		Bucket:               aws.String(bucket),
		WebsiteConfiguration: &types.WebsiteConfiguration{},
	}
	if wc.IndexDocument != "" {
		in.WebsiteConfiguration.IndexDocument = &types.IndexDocument{Suffix: aws.String(wc.IndexDocument)}
	}
	if wc.ErrorDocument != "" {
		in.WebsiteConfiguration.ErrorDocument = &types.ErrorDocument{Key: aws.String(wc.ErrorDocument)}
	}
	if wc.RedirectAllRequestsTo != "" {
		in.WebsiteConfiguration.RedirectAllRequestsTo = &types.RedirectAllRequestsTo{HostName: aws.String(wc.RedirectAllRequestsTo)}
	}
	_, err := c.s3.PutBucketWebsite(ctx, in)
	return err
}

// DeleteWebsite 删除桶的静态网站托管配置。
func (c *Client) DeleteWebsite(ctx context.Context, bucket string) error {
	_, err := c.s3.DeleteBucketWebsite(ctx, &s3.DeleteBucketWebsiteInput{Bucket: aws.String(bucket)})
	return err
}

// GetPolicy 读取桶策略（JSON 字符串）。
func (c *Client) GetPolicy(ctx context.Context, bucket string) (string, error) {
	out, err := c.s3.GetBucketPolicy(ctx, &s3.GetBucketPolicyInput{Bucket: aws.String(bucket)})
	if err != nil {
		return "", err
	}
	return derefString(out.Policy), nil
}

// PutPolicy 写入桶策略（JSON 字符串）。
func (c *Client) PutPolicy(ctx context.Context, bucket, policy string) error {
	_, err := c.s3.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{
		Bucket: aws.String(bucket),
		Policy: aws.String(policy),
	})
	return err
}

// DeletePolicy 删除桶策略。
func (c *Client) DeletePolicy(ctx context.Context, bucket string) error {
	_, err := c.s3.DeleteBucketPolicy(ctx, &s3.DeleteBucketPolicyInput{Bucket: aws.String(bucket)})
	return err
}

// GetBucketTags 读取桶标签。
func (c *Client) GetBucketTags(ctx context.Context, bucket string) (map[string]string, error) {
	out, err := c.s3.GetBucketTagging(ctx, &s3.GetBucketTaggingInput{Bucket: aws.String(bucket)})
	if err != nil {
		return nil, err
	}
	tags := map[string]string{}
	for _, t := range out.TagSet {
		tags[derefString(t.Key)] = derefString(t.Value)
	}
	return tags, nil
}

// PutBucketTags 覆盖写入桶标签。
func (c *Client) PutBucketTags(ctx context.Context, bucket string, tags map[string]string) error {
	tagSet := make([]types.Tag, 0, len(tags))
	for k, v := range tags {
		tagSet = append(tagSet, types.Tag{Key: aws.String(k), Value: aws.String(v)})
	}
	_, err := c.s3.PutBucketTagging(ctx, &s3.PutBucketTaggingInput{
		Bucket:  aws.String(bucket),
		Tagging: &types.Tagging{TagSet: tagSet},
	})
	return err
}

// DeleteBucketTags 删除桶全部标签。
func (c *Client) DeleteBucketTags(ctx context.Context, bucket string) error {
	_, err := c.s3.DeleteBucketTagging(ctx, &s3.DeleteBucketTaggingInput{Bucket: aws.String(bucket)})
	return err
}
