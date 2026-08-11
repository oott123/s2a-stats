// Package httpapi 提供路由、token 鉴权与 JSON 响应。
package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"s2astats/internal/stats"
	"s2astats/internal/web"
)

// statsService 是 handler 依赖的业务接口。
type statsService interface {
	AccountWindows(ctx context.Context) (*stats.AccountsResponse, error)
	WindowUsage(ctx context.Context, accountID int64) (*stats.WindowUsageResponse, error)
	MonthlyUsage(ctx context.Context, accountID int64, year, month int) (*stats.MonthlyUsageResponse, error)
}

// Server 持有业务服务、公共 token 与挂载前缀。
type Server struct {
	svc      statsService
	token    string
	basePath string // 如 /stats，空=挂载到根
}

// New 创建 Server。basePath 形如 /stats（已由 config 校验），空表示挂载到根。
func New(svc statsService, token, basePath string) *Server {
	return &Server{svc: svc, token: token, basePath: basePath}
}

var monthRe = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])$`)

// Handler 构建并返回路由。前端与 v1 API 挂载到 basePath 下，/healthz 始终在根。
func (s *Server) Handler() http.Handler {
	app := http.NewServeMux()
	app.HandleFunc("GET /{$}", s.handleIndex)
	app.Handle("GET /v1/accounts", s.auth(http.HandlerFunc(s.handleAccounts)))
	app.Handle("GET /v1/accounts/{id}/window-usage", s.auth(http.HandlerFunc(s.handleWindowUsage)))
	app.Handle("GET /v1/accounts/{id}/monthly-usage", s.auth(http.HandlerFunc(s.handleMonthlyUsage)))

	root := http.NewServeMux()
	root.HandleFunc("GET /healthz", s.handleHealthz)
	if s.basePath == "" {
		root.Handle("/", app)
	} else {
		root.Handle(s.basePath+"/", http.StripPrefix(s.basePath, app))
		// 无尾斜杠时重定向到 basePath/，确保前端相对路径解析正确。
		root.Handle(s.basePath, http.RedirectHandler(s.basePath+"/", http.StatusMovedPermanently))
	}
	return root
}

// handleIndex 返回内嵌的单页前端。
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(web.IndexHTML)
}

// auth 校验公共只读 token：Authorization: Bearer 或 ?token=。
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provided := r.URL.Query().Get("token")
		if h := r.Header.Get("Authorization"); h != "" {
			if after, ok := strings.CutPrefix(h, "Bearer "); ok {
				provided = after
			}
		}
		if subtle.ConstantTimeCompare([]byte(provided), []byte(s.token)) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAccounts(w http.ResponseWriter, r *http.Request) {
	resp, err := s.svc.AccountWindows(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "upstream error")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleWindowUsage(w http.ResponseWriter, r *http.Request) {
	id, ok := parseAccountID(w, r)
	if !ok {
		return
	}
	resp, err := s.svc.WindowUsage(r.Context(), id)
	if err != nil {
		if errors.Is(err, stats.ErrAccountNotFound) {
			writeError(w, http.StatusNotFound, "account not found")
			return
		}
		writeError(w, http.StatusBadGateway, "upstream error")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleMonthlyUsage(w http.ResponseWriter, r *http.Request) {
	id, ok := parseAccountID(w, r)
	if !ok {
		return
	}
	month := r.URL.Query().Get("month")
	if !monthRe.MatchString(month) {
		writeError(w, http.StatusBadRequest, "invalid month, expected YYYY-MM")
		return
	}
	year, _ := strconv.Atoi(month[:4])
	mon, _ := strconv.Atoi(month[5:7])

	resp, err := s.svc.MonthlyUsage(r.Context(), id, year, mon)
	if err != nil {
		if errors.Is(err, stats.ErrAccountNotFound) {
			writeError(w, http.StatusNotFound, "account not found")
			return
		}
		writeError(w, http.StatusBadGateway, "upstream error")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// parseAccountID 解析路径参数 id，要求正整数；非法时写 400 并返回 false。
func parseAccountID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := r.PathValue("id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid account id")
		return 0, false
	}
	return id, true
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil && !errors.Is(err, http.ErrHandlerTimeout) {
		// 响应已开始写出，无法再改状态码；仅尽力而为。
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
