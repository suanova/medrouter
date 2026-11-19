package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// S3Credentials is a static credential set for the object store. Empty values
// mean anonymous access, which only works for public buckets.
type S3Credentials struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
}

// LoadS3Credentials resolves credentials from explicit environment variables
// first, then from an AWS shared credentials file (S3_CREDENTIALS_FILE). The
// file format is the standard INI:
//
//	[default]
//	aws_access_key_id = ...
//	aws_secret_access_key = ...
//	aws_session_token = ...   # optional
func (c *Config) LoadS3Credentials() (S3Credentials, error) {
	if c.S3AccessKeyID != "" || c.S3SecretAccessKey != "" {
		if c.S3AccessKeyID == "" || c.S3SecretAccessKey == "" {
			return S3Credentials{}, fmt.Errorf("S3_ACCESS_KEY_ID and S3_SECRET_ACCESS_KEY must be set together")
		}
		return S3Credentials{
			AccessKeyID:     c.S3AccessKeyID,
			SecretAccessKey: c.S3SecretAccessKey,
			SessionToken:    c.S3SessionToken,
		}, nil
	}
	if c.S3CredentialsFile == "" {
		return S3Credentials{}, nil
	}
	return parseSharedCredentialsFile(c.S3CredentialsFile, c.S3CredentialsProfile)
}

func parseSharedCredentialsFile(filename, profile string) (S3Credentials, error) {
	file, err := os.Open(filename)
	if err != nil {
		return S3Credentials{}, fmt.Errorf("open S3 credentials file: %w", err)
	}
	defer func() { _ = file.Close() }()

	if profile == "" {
		profile = "default"
	}

	var (
		out        S3Credentials
		current    string
		sawProfile bool
	)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			current = strings.TrimSpace(line[1 : len(line)-1])
			if current == profile {
				sawProfile = true
			}
			continue
		}
		if current != profile {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "aws_access_key_id":
			out.AccessKeyID = strings.TrimSpace(value)
		case "aws_secret_access_key":
			out.SecretAccessKey = strings.TrimSpace(value)
		case "aws_session_token":
			out.SessionToken = strings.TrimSpace(value)
		}
	}
	if err := scanner.Err(); err != nil {
		return S3Credentials{}, fmt.Errorf("read S3 credentials file: %w", err)
	}
	if !sawProfile {
		return S3Credentials{}, fmt.Errorf("S3 credentials file has no [%s] profile", profile)
	}
	if out.AccessKeyID == "" || out.SecretAccessKey == "" {
		return S3Credentials{}, fmt.Errorf("S3 credentials file profile [%s] is incomplete", profile)
	}
	return out, nil
}
