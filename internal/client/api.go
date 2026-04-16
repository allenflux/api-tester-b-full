package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"api-tester/internal/config"
	"api-tester/internal/model"
)

type APIClient struct {
	cfg        *config.Config
	httpClient *http.Client
}

type CallResponse struct {
	StatusCode int
	Body       string
	TaskID     string
	TaskStatus int
}

func New(cfg *config.Config) *APIClient {
	if cfg == nil {
		return &APIClient{
			cfg: nil,
			httpClient: &http.Client{
				Timeout: 30 * time.Second,
			},
		}
	}

	tr := &http.Transport{}
	if cfg.API.InsecureSkipVerify {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	timeoutSec := cfg.Runner.HTTPTimeoutSec
	if timeoutSec <= 0 {
		timeoutSec = 30
	}

	return &APIClient{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout:   time.Duration(timeoutSec) * time.Second,
			Transport: tr,
		},
	}
}

func (c *APIClient) Invoke(ctx context.Context, ep model.Endpoint, payload map[string]any) (*CallResponse, error) {
	if c == nil {
		return nil, errors.New("api client is nil")
	}
	if c.cfg == nil {
		return nil, errors.New("api client config is nil")
	}
	if c.httpClient == nil {
		return nil, errors.New("http client is nil")
	}
	if strings.TrimSpace(c.cfg.API.BaseURL) == "" {
		return nil, errors.New("api.base_url is empty")
	}

	method := strings.ToUpper(strings.TrimSpace(ep.Method))
	if method == "" {
		method = http.MethodPost
	}

	u := strings.TrimRight(c.cfg.API.BaseURL, "/") + ep.Path

	var req *http.Request
	var err error

	switch method {
	case http.MethodGet, http.MethodDelete:
		q := url.Values{}
		for k, v := range payload {
			q.Set(k, fmt.Sprint(v))
		}
		if enc := q.Encode(); enc != "" {
			if strings.Contains(u, "?") {
				u += "&" + enc
			} else {
				u += "?" + enc
			}
		}
		req, err = http.NewRequestWithContext(ctx, method, u, nil)

	default:
		contentType, body, bodyErr := buildBody(ep, payload)
		if bodyErr != nil {
			return nil, bodyErr
		}
		req, err = http.NewRequestWithContext(ctx, method, u, body)
		if err == nil && req != nil {
			req.Header.Set("Content-Type", contentType)
		}
	}

	if err != nil {
		return nil, err
	}
	if req == nil {
		return nil, errors.New("request is nil after build")
	}
	if req.Header == nil {
		req.Header = make(http.Header)
	}

	apiKeyHeader := strings.TrimSpace(c.cfg.API.APIKeyHeader)
	if apiKeyHeader == "" {
		apiKeyHeader = "Apikey"
	}
	req.Header.Set(apiKeyHeader, c.cfg.API.APIKey)

	for k, v := range c.cfg.API.Headers {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	cr := &CallResponse{
		StatusCode: resp.StatusCode,
		Body:       string(raw),
		TaskStatus: -1,
	}

	var obj map[string]any
	if json.Unmarshal(raw, &obj) == nil {
		cr.TaskID = findStringByKeys(obj, "task_id", "uuid", "id")
	}

	return cr, nil
}

func (c *APIClient) PollTask(ctx context.Context, taskID string) (int, string, error) {
	if c == nil {
		return -1, "", errors.New("api client is nil")
	}
	if c.cfg == nil {
		return -1, "", errors.New("api client config is nil")
	}
	if c.httpClient == nil {
		return -1, "", errors.New("http client is nil")
	}

	base := strings.TrimRight(c.cfg.API.BaseURL, "/") + c.cfg.API.TaskStatusPath
	method := strings.ToUpper(strings.TrimSpace(c.cfg.API.TaskStatusMethod))
	if method == "" {
		method = http.MethodGet
	}

	var req *http.Request
	var err error

	if method == http.MethodGet {
		u := base + "?" + url.Values{
			c.cfg.API.TaskStatusIDField: []string{taskID},
		}.Encode()
		req, err = http.NewRequestWithContext(ctx, method, u, nil)
	} else {
		bodyMap := map[string]string{c.cfg.API.TaskStatusIDField: taskID}
		buf, _ := json.Marshal(bodyMap)
		req, err = http.NewRequestWithContext(ctx, method, base, bytes.NewReader(buf))
		if err == nil && req != nil {
			req.Header.Set("Content-Type", "application/json")
		}
	}

	if err != nil {
		return -1, "", err
	}
	if req == nil {
		return -1, "", errors.New("task poll request is nil")
	}
	if req.Header == nil {
		req.Header = make(http.Header)
	}

	apiKeyHeader := strings.TrimSpace(c.cfg.API.APIKeyHeader)
	if apiKeyHeader == "" {
		apiKeyHeader = "Apikey"
	}
	req.Header.Set(apiKeyHeader, c.cfg.API.APIKey)

	for k, v := range c.cfg.API.Headers {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return -1, "", err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	body := string(raw)

	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return -1, body, err
	}
	status := intValue(obj[c.cfg.API.TaskStatusValueKey])
	return status, body, nil
}
