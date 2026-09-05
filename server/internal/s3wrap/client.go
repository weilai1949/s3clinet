package s3wrap

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go/middleware"

	"github.com/weilai1949/s3clinet/server/internal/model"
)

var (
	sharedHTTP     *ssrfAwareClient
	sharedHTTPOnce sync.Once
)

// Client 对某个账号的 S3 操作做薄封装，方便复用 SDK 接口。
type Client struct {
	acc     *model.Account
	s3      *s3.Client
	presign *s3.PresignClient
}

// New 根据账号构建 S3 客户端与预签名客户端。
func New(acc *model.Account) (*Client, error) {
	if acc == nil {
		return nil, errors.New("empty account")
	}
	if acc.AccessKey == "" || acc.SecretKey == "" {
		return nil, errors.New("missing access key or secret key")
	}
	if err := ValidateEndpoint(acc.Endpoint); err != nil {
		return nil, fmt.Errorf("endpoint: %w", err)
	}
	if err := ValidateEndpoint(acc.PublicEndpoint); err != nil {
		return nil, fmt.Errorf("publicEndpoint: %w", err)
	}
	creds := credentials.NewStaticCredentialsProvider(acc.AccessKey, acc.SecretKey, "")
	region := acc.Region
	if region == "" {
		region = "us-east-1"
	}
	cfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(creds),
		awsconfig.WithHTTPClient(sharedHTTPClient()),
	)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	svc := newS3FromConfig(cfg, acc, acc.Endpoint)
	presignEndpoint := acc.PublicEndpoint
	if presignEndpoint == "" {
		presignEndpoint = acc.Endpoint
	}
	presignSvc := newS3FromConfig(cfg, acc, presignEndpoint)
	return &Client{acc: acc, s3: svc, presign: s3.NewPresignClient(presignSvc)}, nil
}

func newS3FromConfig(cfg aws.Config, acc *model.Account, endpoint string) *s3.Client {
	return s3.NewFromConfig(cfg, func(o *s3.Options) {
		if ep := normalizeEndpoint(endpoint, acc.UseSSL); ep != "" {
			o.BaseEndpoint = aws.String(ep)
		}
		o.UsePathStyle = acc.PathStyle
		o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		o.APIOptions = append(o.APIOptions, func(stack *middleware.Stack) error {
			return stack.Finalize.Insert(&unsignedPayloadSetter{}, "ResolveEndpointV2", middleware.After)
		})
	})
}

const unsignedPayload = "UNSIGNED-PAYLOAD"

type unsignedPayloadSetter struct{}

func (m *unsignedPayloadSetter) ID() string { return "s3clinet:unsigned-payload" }

func (m *unsignedPayloadSetter) HandleFinalize(ctx context.Context, in middleware.FinalizeInput, next middleware.FinalizeHandler) (middleware.FinalizeOutput, middleware.Metadata, error) {
	ctx = v4.SetPayloadHash(ctx, unsignedPayload)
	return next.HandleFinalize(ctx, in)
}

func sharedHTTPClient() *ssrfAwareClient {
	sharedHTTPOnce.Do(func() {
		sharedHTTP = newHTTPClient()
	})
	return sharedHTTP
}

// ssrfAwareClient：禁重定向 + Dial 时拦截链路本地/云元数据 IP。
type ssrfAwareClient struct {
	client *http.Client
}

func (c *ssrfAwareClient) Do(req *http.Request) (*http.Response, error) {
	return c.client.Do(req)
}

func newHTTPClient() *ssrfAwareClient {
	inner := awshttp.NewBuildableClient().
		WithTransportOptions(func(tr *http.Transport) {
			tr.DialContext = dialContextSSRF
			tr.TLSHandshakeTimeout = 10 * time.Second
			tr.ResponseHeaderTimeout = 30 * time.Second
			tr.ExpectContinueTimeout = 5 * time.Second
			tr.IdleConnTimeout = 90 * time.Second
			tr.MaxIdleConns = 128
			tr.MaxIdleConnsPerHost = 32
			// 安全关键：禁用 HTTP(S)_PROXY 环境变量，避免 S3 出站被代理到任意主机
			// （绕过 dialContextSSRF 的 IP 黑名单）。SSRF 防护只在直连下成立。
			tr.Proxy = nil
		})
	return &ssrfAwareClient{
		client: &http.Client{
			Transport:     inner.GetTransport(),
			CheckRedirect: checkRedirectDenied,
		},
	}
}

// normalizeEndpoint 补全 scheme。若 endpoint 已含 scheme 则原样返回。
func normalizeEndpoint(endpoint string, useSSL bool) string {
	if endpoint == "" {
		return ""
	}
	if strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://") {
		return strings.TrimRight(endpoint, "/")
	}
	scheme := "http"
	if useSSL {
		scheme = "https"
	}
	return scheme + "://" + strings.TrimRight(endpoint, "/")
}

// S3 returns the underlying SDK client (advanced use).
func (c *Client) S3() *s3.Client { return c.s3 }
