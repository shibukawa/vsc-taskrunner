package web

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func newS3Client(ctx context.Context, options ObjectIndexStoreOptions) (*s3.Client, error) {
	loadOptions := []func(*config.LoadOptions) error{
		config.WithRegion(options.Region),
	}
	if strings.TrimSpace(options.AccessKey) != "" || strings.TrimSpace(options.SecretKey) != "" {
		loadOptions = append(loadOptions, config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(options.AccessKey, options.SecretKey, "")))
	}
	cfg, err := config.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	return s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = options.ForcePathStyle
		if strings.TrimSpace(options.Endpoint) != "" {
			o.BaseEndpoint = aws.String(options.Endpoint)
		}
	}), nil
}
