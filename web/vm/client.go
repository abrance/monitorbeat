// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License

// Package vm 封装 VictoriaMetrics PromQL HTTP API 的查询客户端。
package vm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Point 是 VM 返回的 [timestampSeconds, valueString] 单点。
type Point [2]float64

// UnmarshalJSON 解析 VM 的 [ts, "val"] 形态（value 可能为字符串或数字）。
func (p *Point) UnmarshalJSON(b []byte) error {
	var raw [2]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	var ts float64
	if err := json.Unmarshal(raw[0], &ts); err != nil {
		return err
	}
	var vs string
	if err := json.Unmarshal(raw[1], &vs); err != nil {
		var vn float64
		if err2 := json.Unmarshal(raw[1], &vn); err2 != nil {
			return err
		}
		p[0], p[1] = ts, vn
		return nil
	}
	v, err := strconv.ParseFloat(vs, 64)
	if err != nil {
		return err
	}
	p[0], p[1] = ts, v
	return nil
}

// Series 是带 label 的区间时序。
type Series struct {
	Metric map[string]string `json:"metric"`
	Values []Point           `json:"values"`
}

// Vector 是 instant query 的单点结果。
type Vector struct {
	Metric map[string]string `json:"metric"`
	Value  Point             `json:"value"`
}

type queryResponse struct {
	Status string          `json:"status"`
	Data   json.RawMessage `json:"data"`
	Error  string          `json:"error"`
}

// queryData 是 instant/range query 的 data 形态（data.result 为结果数组）。
type queryData struct {
	ResultType string          `json:"resultType"`
	Result     json.RawMessage `json:"result"`
}

// Client 封装 VictoriaMetrics PromQL HTTP API。
type Client struct {
	baseURL string
	http    *http.Client
}

// New 构造 VM 客户端。
func New(baseURL string, timeout time.Duration) *Client {
	return &Client{
		baseURL: baseURL,
		http: &http.Client{
			Timeout:   timeout,
			Transport: &http.Transport{},
		},
	}
}

func (c *Client) doQuery(ctx context.Context, path string, q url.Values) (*queryResponse, error) {
	u := c.baseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vm request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("vm http %d: %s", resp.StatusCode, string(body))
	}
	var out queryResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("vm decode: %w", err)
	}
	if out.Status != "success" {
		return nil, fmt.Errorf("vm error: %s", out.Error)
	}
	return &out, nil
}

// Query 执行 instant query，返回 vector 结果。
func (c *Client) Query(ctx context.Context, expr string) ([]Vector, error) {
	out, err := c.doQuery(ctx, "/api/v1/query", url.Values{"query": {expr}})
	if err != nil {
		return nil, err
	}
	var d queryData
	if err := json.Unmarshal(out.Data, &d); err != nil {
		return nil, err
	}
	if d.ResultType != "vector" {
		return nil, nil
	}
	var res []Vector
	if err := json.Unmarshal(d.Result, &res); err != nil {
		return nil, err
	}
	return res, nil
}

// QueryRange 执行区间查询，返回 matrix 结果。
func (c *Client) QueryRange(ctx context.Context, expr, start, end, step string) ([]Series, error) {
	q := url.Values{"query": {expr}, "start": {start}, "end": {end}, "step": {step}}
	out, err := c.doQuery(ctx, "/api/v1/query_range", q)
	if err != nil {
		return nil, err
	}
	var d queryData
	if err := json.Unmarshal(out.Data, &d); err != nil {
		return nil, err
	}
	if d.ResultType != "matrix" {
		return nil, nil
	}
	var res []Series
	if err := json.Unmarshal(d.Result, &res); err != nil {
		return nil, err
	}
	return res, nil
}

// LabelValues 返回某 label 的取值列表。VM 此处 data 为普通数组（非 resultType/result 形态）。
func (c *Client) LabelValues(ctx context.Context, label string) ([]string, error) {
	out, err := c.doQuery(ctx, "/api/v1/label/"+label+"/values", nil)
	if err != nil {
		return nil, err
	}
	var res []string
	if err := json.Unmarshal(out.Data, &res); err != nil {
		return nil, err
	}
	return res, nil
}
