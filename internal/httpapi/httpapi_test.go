package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"s2astats/internal/stats"
)

type fakeSvc struct {
	monthlyCalled bool
	gotYear       int
	gotMonth      int
}

func (f *fakeSvc) AccountWindows(context.Context) (*stats.AccountsResponse, error) {
	return &stats.AccountsResponse{Accounts: []stats.AccountDTO{}}, nil
}

func (f *fakeSvc) WindowUsage(context.Context, int64) (*stats.WindowUsageResponse, error) {
	return &stats.WindowUsageResponse{}, nil
}

func (f *fakeSvc) MonthlyUsage(_ context.Context, _ int64, year, month int) (*stats.MonthlyUsageResponse, error) {
	f.monthlyCalled = true
	f.gotYear, f.gotMonth = year, month
	return &stats.MonthlyUsageResponse{}, nil
}

const testToken = "secret-token"

func do(t *testing.T, h http.Handler, method, target string, header map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHealthzNoAuth(t *testing.T) {
	h := New(&fakeSvc{}, testToken, "").Handler()
	rec := do(t, h, "GET", "/healthz", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz code = %d, want 200", rec.Code)
	}
}

func TestAuth(t *testing.T) {
	h := New(&fakeSvc{}, testToken, "").Handler()
	tests := []struct {
		name   string
		target string
		header map[string]string
		want   int
	}{
		{"无 token", "/v1/accounts", nil, http.StatusUnauthorized},
		{"错 token header", "/v1/accounts", map[string]string{"Authorization": "Bearer wrong"}, http.StatusUnauthorized},
		{"错 token query", "/v1/accounts?token=wrong", nil, http.StatusUnauthorized},
		{"对 token header", "/v1/accounts", map[string]string{"Authorization": "Bearer " + testToken}, http.StatusOK},
		{"对 token query", "/v1/accounts?token=" + testToken, nil, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(t, h, "GET", tt.target, tt.header)
			if rec.Code != tt.want {
				t.Errorf("code = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}

func TestBasePath(t *testing.T) {
	h := New(&fakeSvc{}, testToken, "/stats").Handler()
	tests := []struct {
		name   string
		target string
		header map[string]string
		want   int
	}{
		{"前端首页", "/stats/", nil, http.StatusOK},
		{"无尾斜杠重定向", "/stats", nil, http.StatusMovedPermanently},
		{"前缀下的 API", "/stats/v1/accounts", map[string]string{"Authorization": "Bearer " + testToken}, http.StatusOK},
		{"根路径 API 不存在", "/v1/accounts", map[string]string{"Authorization": "Bearer " + testToken}, http.StatusNotFound},
		{"healthz 仍在根", "/healthz", nil, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(t, h, "GET", tt.target, tt.header)
			if rec.Code != tt.want {
				t.Errorf("%s: code = %d, want %d", tt.target, rec.Code, tt.want)
			}
		})
	}
}

func TestIndexServed(t *testing.T) {
	h := New(&fakeSvc{}, testToken, "").Handler()
	rec := do(t, h, "GET", "/", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("index code = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content-type = %q, want text/html", ct)
	}
}

func TestAccountID(t *testing.T) {
	h := New(&fakeSvc{}, testToken, "").Handler()
	tests := []struct {
		id   string
		want int
	}{
		{"123", http.StatusOK},
		{"0", http.StatusBadRequest},
		{"-1", http.StatusBadRequest},
		{"abc", http.StatusBadRequest},
	}
	for _, tt := range tests {
		rec := do(t, h, "GET", "/v1/accounts/"+tt.id+"/window-usage?token="+testToken, nil)
		if rec.Code != tt.want {
			t.Errorf("id %q: code = %d, want %d", tt.id, rec.Code, tt.want)
		}
	}
}

func TestMonthValidation(t *testing.T) {
	tests := []struct {
		month     string
		want      int
		wantYear  int
		wantMonth int
	}{
		{"2026-06", http.StatusOK, 2026, 6},
		{"2026-12", http.StatusOK, 2026, 12},
		{"2026-13", http.StatusBadRequest, 0, 0},
		{"2026-00", http.StatusBadRequest, 0, 0},
		{"2026-6", http.StatusBadRequest, 0, 0},
		{"26-06", http.StatusBadRequest, 0, 0},
		{"", http.StatusBadRequest, 0, 0},
	}
	for _, tt := range tests {
		fake := &fakeSvc{}
		h := New(fake, testToken, "").Handler()
		rec := do(t, h, "GET", "/v1/accounts/1/monthly-usage?token="+testToken+"&month="+tt.month, nil)
		if rec.Code != tt.want {
			t.Errorf("month %q: code = %d, want %d", tt.month, rec.Code, tt.want)
			continue
		}
		if tt.want == http.StatusOK {
			if !fake.monthlyCalled || fake.gotYear != tt.wantYear || fake.gotMonth != tt.wantMonth {
				t.Errorf("month %q: parsed (%d,%d), want (%d,%d)", tt.month, fake.gotYear, fake.gotMonth, tt.wantYear, tt.wantMonth)
			}
		}
	}
}

func TestErrorEnvelope(t *testing.T) {
	h := New(&fakeSvc{}, testToken, "").Handler()
	rec := do(t, h, "GET", "/v1/accounts", nil)
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["error"] != "unauthorized" {
		t.Errorf("error = %q, want unauthorized", body["error"])
	}
}
