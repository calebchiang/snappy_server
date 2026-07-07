package services

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

var ErrR2ConfigMissing = errors.New("r2 configuration missing")

type R2UploadResult struct {
	Key string
	URL string
}

func UploadWordImage(
	ctx context.Context,
	userID uint,
	imageBytes []byte,
	contentType string,
	extension string,
) (R2UploadResult, error) {
	config, err := loadR2Config()
	if err != nil {
		return R2UploadResult{}, err
	}

	objectID, err := randomHex(16)
	if err != nil {
		return R2UploadResult{}, err
	}

	key := fmt.Sprintf("users/%d/words/%s%s", userID, objectID, extension)
	uploadURL, err := r2ObjectURL(config.Endpoint, config.BucketName, key)
	if err != nil {
		return R2UploadResult{}, err
	}

	if err := putR2Object(ctx, config, uploadURL, imageBytes, contentType); err != nil {
		return R2UploadResult{}, err
	}

	return R2UploadResult{
		Key: key,
		URL: r2PublicURL(config, key, uploadURL),
	}, nil
}

func DeleteWordImage(ctx context.Context, key string) error {
	config, err := loadR2Config()
	if err != nil {
		return err
	}

	objectURL, err := r2ObjectURL(config.Endpoint, config.BucketName, key)
	if err != nil {
		return err
	}

	return deleteR2Object(ctx, config, objectURL)
}

type r2Config struct {
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
	BucketName      string
	PublicURL       string
}

func loadR2Config() (r2Config, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(os.Getenv("R2_ENDPOINT")), "/")
	if endpoint == "" {
		accountID := strings.TrimSpace(os.Getenv("R2_ACCOUNT_ID"))
		if accountID != "" {
			endpoint = fmt.Sprintf("https://%s.r2.cloudflarestorage.com", accountID)
		}
	}

	config := r2Config{
		Endpoint:        endpoint,
		AccessKeyID:     strings.TrimSpace(os.Getenv("R2_ACCESS_KEY_ID")),
		SecretAccessKey: strings.TrimSpace(os.Getenv("R2_SECRET_ACCESS_KEY")),
		BucketName:      strings.TrimSpace(os.Getenv("R2_BUCKET_NAME")),
		PublicURL:       strings.TrimRight(strings.TrimSpace(os.Getenv("R2_PUBLIC_URL")), "/"),
	}

	if config.Endpoint == "" ||
		config.AccessKeyID == "" ||
		config.SecretAccessKey == "" ||
		config.BucketName == "" {
		return r2Config{}, ErrR2ConfigMissing
	}

	return config, nil
}

func r2ObjectURL(endpoint string, bucketName string, key string) (*url.URL, error) {
	baseURL, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}

	escapedParts := make([]string, 0, len(strings.Split(key, "/"))+1)
	escapedParts = append(escapedParts, url.PathEscape(bucketName))
	for _, part := range strings.Split(key, "/") {
		escapedParts = append(escapedParts, url.PathEscape(part))
	}

	baseURL.Path = "/" + strings.Join(escapedParts, "/")
	baseURL.RawQuery = ""

	return baseURL, nil
}

func putR2Object(
	ctx context.Context,
	config r2Config,
	uploadURL *url.URL,
	imageBytes []byte,
	contentType string,
) error {
	bodyHash := sha256Hex(imageBytes)
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPut,
		uploadURL.String(),
		bytes.NewReader(imageBytes),
	)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Amz-Content-Sha256", bodyHash)
	req.Header.Set("X-Amz-Date", amzDate)
	req.ContentLength = int64(len(imageBytes))

	credentialScope := dateStamp + "/auto/s3/aws4_request"
	canonicalHeaders := "host:" + uploadURL.Host + "\n" +
		"x-amz-content-sha256:" + bodyHash + "\n" +
		"x-amz-date:" + amzDate + "\n"
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"
	canonicalRequest := strings.Join([]string{
		http.MethodPut,
		uploadURL.EscapedPath(),
		"",
		canonicalHeaders,
		signedHeaders,
		bodyHash,
	}, "\n")

	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		credentialScope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")

	signingKey := sigV4SigningKey(config.SecretAccessKey, dateStamp, "auto", "s3")
	signature := hex.EncodeToString(hmacSHA256(signingKey, stringToSign))
	authorization := fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		config.AccessKeyID,
		credentialScope,
		signedHeaders,
		signature,
	)
	req.Header.Set("Authorization", authorization)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return fmt.Errorf("r2 upload failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
}

func deleteR2Object(
	ctx context.Context,
	config r2Config,
	objectURL *url.URL,
) error {
	bodyHash := sha256Hex(nil)
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodDelete,
		objectURL.String(),
		nil,
	)
	if err != nil {
		return err
	}

	req.Header.Set("X-Amz-Content-Sha256", bodyHash)
	req.Header.Set("X-Amz-Date", amzDate)

	credentialScope := dateStamp + "/auto/s3/aws4_request"
	canonicalHeaders := "host:" + objectURL.Host + "\n" +
		"x-amz-content-sha256:" + bodyHash + "\n" +
		"x-amz-date:" + amzDate + "\n"
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"
	canonicalRequest := strings.Join([]string{
		http.MethodDelete,
		objectURL.EscapedPath(),
		"",
		canonicalHeaders,
		signedHeaders,
		bodyHash,
	}, "\n")

	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		credentialScope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")

	signingKey := sigV4SigningKey(config.SecretAccessKey, dateStamp, "auto", "s3")
	signature := hex.EncodeToString(hmacSHA256(signingKey, stringToSign))
	authorization := fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		config.AccessKeyID,
		credentialScope,
		signedHeaders,
		signature,
	)
	req.Header.Set("Authorization", authorization)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return fmt.Errorf("r2 delete failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
}

func r2PublicURL(config r2Config, key string, uploadURL *url.URL) string {
	if config.PublicURL != "" {
		escapedParts := make([]string, 0, len(strings.Split(key, "/")))
		for _, part := range strings.Split(key, "/") {
			escapedParts = append(escapedParts, url.PathEscape(part))
		}
		return config.PublicURL + "/" + strings.Join(escapedParts, "/")
	}

	return uploadURL.String()
}

func randomHex(byteCount int) (string, error) {
	buffer := make([]byte, byteCount)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

func sha256Hex(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func sigV4SigningKey(secret string, dateStamp string, region string, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), dateStamp)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, service)
	return hmacSHA256(kService, "aws4_request")
}

func hmacSHA256(key []byte, value string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(value))
	return mac.Sum(nil)
}
