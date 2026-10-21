package main

import (
	"io/fs"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
)

// ---------------------------------------------------------------------------
// - 路由注册
// ---------------------------------------------------------------------------

// newRouter 组装全部路由：公开认证端点、需认证的业务 API、静态文件服务
func newRouter() http.Handler {
	mux := http.NewServeMux()

	// Auth 是公开端点（challenge 和 login 不需要认证）
	mux.HandleFunc("/api/auth", handleAuth)

	// 需要认证的 API
	authAPIs := map[string]http.HandlerFunc{
		"/api/get_todos":     handleGetTodos,
		"/api/get_history":   handleGetHistory,
		"/api/add_todo":      handleAddTodo,
		"/api/update_todo":   handleUpdateTodo,
		"/api/complete_todo": handleCompleteTodo,
		"/api/delete_todo":   handleDeleteTodo,
		"/api/reorder_todos": handleReorderTodos,
		"/api/get_timeline":  handleGetTimeline,
		"/api/save_timeline": handleSaveTimeline,
		"/api/export_csv":    handleExportCSV,
		"/api/export_json":   handleExportJSON,
		"/api/import_json":   handleImportJSON,
	}

	for path, handler := range authAPIs {
		p := path
		h := handler
		mux.HandleFunc(p, requireAuth(requireCsrf(h)))
	}

	// 静态文件 / 页面路由
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		handlePage(w, r)
	})

	return mux
}

// ---------------------------------------------------------------------------
// - 静态文件服务
// ---------------------------------------------------------------------------

func handlePage(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "/" || path == "/index" {
		path = "/index.html"
	} else if path == "/login" {
		path = "/login.html"
	}

	subFS, err := fs.Sub(webFS, "web")
	if err != nil {
		http.Error(w, "500", http.StatusInternalServerError)
		return
	}

	fullPath := strings.TrimPrefix(path, "/")
	data, err := fs.ReadFile(subFS, fullPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	contentType := "text/html; charset=utf-8"
	if ext := filepath.Ext(path); ext != "" {
		if t := mime.TypeByExtension(ext); t != "" {
			contentType = t
		}
	}

	w.Header().Set("Content-Type", contentType)
	w.Write(data)
}
