package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	tr := &http.Transport{}
	if cfg != nil && cfg.API.InsecureSkipVerify {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	timeoutSec := 30
	if cfg != nil && cfg.Runner.HTTPTimeoutSec > 0 {
		timeoutSec = cfg.Runner.HTTPTimeoutSec
	}
	return &APIClient{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: time.Duration(timeoutSec) * time.Second, Transport: tr},
	}
}

func (c *APIClient) Invoke(ctx context.Context, ep model.Endpoint, payload map[string]any) (*CallResponse, error) {
	if c == nil || c.cfg == nil || c.httpClient == nil {
		return nil, errors.New("api client not initialized")
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
		contentType, body, bodyErr := buildBody(payload)
		if bodyErr != nil {
			return nil, bodyErr
		}
		req, err = http.NewRequestWithContext(ctx, method, u, body)
		if err == nil {
			req.Header.Set("Content-Type", contentType)
		}
	}
	if err != nil {
		return nil, err
	}
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	apiKeyHeader := strings.TrimSpace(c.cfg.API.APIKeyHeader)
	if apiKeyHeader == "" {
		apiKeyHeader = "Apikey"
	}
	req.Header.Set(apiKeyHeader, c.cfg.API.APIKey)
	req.Header.Set("Accept", "application/json")
	for k, v := range c.cfg.API.Headers {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))

	cr := &CallResponse{StatusCode: resp.StatusCode, Body: string(raw), TaskStatus: -1}
	var obj map[string]any
	if json.Unmarshal(raw, &obj) == nil {
		cr.TaskID = findStringByKeys(obj, "task_id", "uuid", "id")
	}
	return cr, nil
}

func (c *APIClient) PollTask(ctx context.Context, taskID string) (int, string, error) {
	if c == nil || c.cfg == nil || c.httpClient == nil {
		return -1, "", errors.New("api client not initialized")
	}
	if strings.TrimSpace(c.cfg.API.BaseURL) == "" {
		return -1, "", errors.New("api.base_url is empty")
	}
	path := strings.TrimSpace(c.cfg.API.TaskStatusPath)
	if path == "" {
		return -1, "", errors.New("api.task_status_path is empty")
	}
	base := strings.TrimRight(c.cfg.API.BaseURL, "/") + path
	idField := strings.TrimSpace(c.cfg.API.TaskStatusIDField)
	if idField == "" {
		idField = "task_id"
	}
	method := strings.ToUpper(strings.TrimSpace(c.cfg.API.TaskStatusMethod))
	if method == "" {
		method = http.MethodGet
	}
	var req *http.Request
	var err error
	if method == http.MethodGet {
		u := base + "?" + url.Values{idField: []string{taskID}}.Encode()
		req, err = http.NewRequestWithContext(ctx, method, u, nil)
	} else {
		vals := url.Values{}
		vals.Set(idField, taskID)
		req, err = http.NewRequestWithContext(ctx, method, base, strings.NewReader(vals.Encode()))
		if err == nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	}
	if err != nil {
		return -1, "", err
	}
	apiKeyHeader := strings.TrimSpace(c.cfg.API.APIKeyHeader)
	if apiKeyHeader == "" {
		apiKeyHeader = "Apikey"
	}
	req.Header.Set(apiKeyHeader, c.cfg.API.APIKey)
	req.Header.Set("Accept", "application/json")
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
	statusKey := strings.TrimSpace(c.cfg.API.TaskStatusValueKey)
	if statusKey == "" {
		statusKey = "status"
	}
	status := intValue(obj[statusKey])
	return status, body, nil
}

func buildBody(payload map[string]any) (string, io.Reader, error) {
	vals := url.Values{}
	for k, v := range payload {
		vals.Set(k, fmt.Sprint(v))
	}
	encoded := vals.Encode()
	return "application/x-www-form-urlencoded", strings.NewReader(encoded), nil
}

func intValue(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case float32:
		return int(t)
	case int:
		return t
	case int64:
		return int(t)
	case int32:
		return int(t)
	case json.Number:
		i, err := t.Int64()
		if err == nil {
			return int(i)
		}
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(t))
		if err == nil {
			return i
		}
	}
	return -1
}

func findStringByKeys(obj map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := obj[k]; ok {
			s := strings.TrimSpace(fmt.Sprint(v))
			if s != "" && s != "<nil>" {
				return s
			}
		}
	}
	for _, v := range obj {
		if inner, ok := v.(map[string]any); ok {
			if s := findStringByKeys(inner, keys...); s != "" {
				return s
			}
		}
	}
	return ""
}

var _ = bytes.MinRead
