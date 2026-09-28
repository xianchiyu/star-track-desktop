package main

import (
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// ---------------------------------------------------------------------------
// embed 前端静态文件
// ---------------------------------------------------------------------------

//go:embed web/*
var webFS embed.FS

// ---------------------------------------------------------------------------
// 全局状态
// ---------------------------------------------------------------------------

var db *sql.DB

// 允许的任务类型
var allowedTaskTypes = map[string]bool{
	"self": true, "family": true, "money": true,
	"sport": true, "love": true, "study": true,
}

// ---------------------------------------------------------------------------
// 工具函数
// ---------------------------------------------------------------------------

func loadEnv() {
	data, err := os.ReadFile(filepath.Join(cfg.DataDir, ".env"))
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	migrated := false
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			k := strings.TrimSpace(parts[0])
			v := strings.TrimSpace(parts[1])
			switch k {
			case "AUTH_USER":
				authUser = v
			case "AUTH_PASS":
				if isHexHash(v) {
					// 已是哈希存储
					authPassHash = v
				} else {
					// 明文密码：迁移为哈希并写回 .env
					authPassHash = hashPassword(v)
					lines[i] = "AUTH_PASS=" + authPassHash
					migrated = true
				}
			case "LISTEN_ADDR":
				cfg.ListenAddr = v
			}
		}
	}
	if migrated {
		if err := os.WriteFile(filepath.Join(cfg.DataDir, ".env"), []byte(strings.Join(lines, "\n")), 0644); err == nil {
			log.Println("检测到明文密码，已自动迁移为哈希存储")
		}
	}
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func jsonSuccess(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data":    data,
	})
}

// ---------------------------------------------------------------------------
// 数据库初始化
// ---------------------------------------------------------------------------

func initDB() error {
	var err error
	dbPath := filepath.Join(cfg.DataDir, "data", "todo.db")
	os.MkdirAll(filepath.Dir(dbPath), 0755)

	db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("打开数据库失败: %w", err)
	}

	db.SetMaxOpenConns(1)

	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA foreign_keys=ON")

	schema := `
	CREATE TABLE IF NOT EXISTS todos (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT NOT NULL,
		task_type TEXT DEFAULT 'self',
		parent_id INTEGER DEFAULT NULL,
		sort_order INTEGER DEFAULT 0,
		progress INTEGER DEFAULT NULL,
		start_date TEXT DEFAULT NULL,
		due_date TEXT DEFAULT NULL,
		completed INTEGER DEFAULT 0,
		completed_at TEXT DEFAULT NULL,
		created_at TEXT DEFAULT (datetime('now','localtime')),
		FOREIGN KEY (parent_id) REFERENCES todos(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS progress_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		todo_id INTEGER NOT NULL,
		log_date TEXT NOT NULL,
		progress INTEGER NOT NULL,
		created_at TEXT DEFAULT (datetime('now','localtime')),
		FOREIGN KEY (todo_id) REFERENCES todos(id) ON DELETE CASCADE
	);

	CREATE UNIQUE INDEX IF NOT EXISTS idx_progress_log_unique
		ON progress_log (todo_id, log_date);

	CREATE TABLE IF NOT EXISTS timeline_slots (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		todo_id INTEGER NOT NULL,
		slot_date TEXT NOT NULL,
		slot_hour INTEGER NOT NULL,
		created_at TEXT DEFAULT (datetime('now','localtime')),
		FOREIGN KEY (todo_id) REFERENCES todos(id) ON DELETE CASCADE
	);

	CREATE UNIQUE INDEX IF NOT EXISTS idx_timeline_unique
		ON timeline_slots (todo_id, slot_date, slot_hour);
	`
	_, err = db.Exec(schema)
	if err != nil {
		return fmt.Errorf("建表失败: %w", err)
	}

	return nil
}

// ---------------------------------------------------------------------------
// - History
// ---------------------------------------------------------------------------

type HistoryItem struct {
	ID          int     `json:"id"`
	Title       string  `json:"title"`
	TaskType    string  `json:"task_type"`
	ParentID    *int    `json:"parent_id"`
	DueDate     *string `json:"due_date"`
	StartDate   *string `json:"start_date"`
	CompletedAt *string `json:"completed_at"`
	Progress    int     `json:"progress"`
	Type        string  `json:"type"`
}

func handleGetHistory(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`
		SELECT id, title, task_type, parent_id, due_date, start_date, completed_at, progress
		FROM todos
		WHERE completed = 1 AND completed_at IS NOT NULL
		ORDER BY completed_at DESC
	`)
	if err != nil {
		jsonError(w, "查询失败", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var completedTodos []HistoryItem
	for rows.Next() {
		var item HistoryItem
		var pid sql.NullInt64
		var dd, sd, ca sql.NullString
		var pg sql.NullInt64
		if err := rows.Scan(&item.ID, &item.Title, &item.TaskType, &pid, &dd, &sd, &ca, &pg); err != nil {
			continue
		}
		if pid.Valid {
			v := int(pid.Int64)
			item.ParentID = &v
		}
		if dd.Valid {
			item.DueDate = &dd.String
		}
		if sd.Valid {
			item.StartDate = &sd.String
		}
		if ca.Valid {
			item.CompletedAt = &ca.String
		}
		if pg.Valid {
			item.Progress = int(pg.Int64)
		}
		item.Type = "completed"
		completedTodos = append(completedTodos, item)
	}

	logRows, err := db.Query(`
		SELECT pl.todo_id, pl.log_date, pl.progress,
		       t.title, t.task_type, t.parent_id, t.due_date, t.start_date, t.completed, t.completed_at
		FROM progress_log pl
		JOIN todos t ON t.id = pl.todo_id
		ORDER BY pl.log_date DESC, pl.created_at DESC
	`)
	if err == nil {
		defer logRows.Close()

		type ProgressEntry struct {
			TodoID      int
			LogDate     string
			Progress    int
			Title       string
			TaskType    string
			ParentID    sql.NullInt64
			DueDate     sql.NullString
			StartDate   sql.NullString
			Completed   int
			CompletedAt sql.NullString
		}

		var progressLogs []ProgressEntry
		for logRows.Next() {
			var pe ProgressEntry
			if err := logRows.Scan(&pe.TodoID, &pe.LogDate, &pe.Progress,
				&pe.Title, &pe.TaskType, &pe.ParentID, &pe.DueDate, &pe.StartDate, &pe.Completed, &pe.CompletedAt); err != nil {
				continue
			}
			progressLogs = append(progressLogs, pe)
		}

		grouped := map[string]map[int]HistoryItem{}

		for _, t := range completedTodos {
			date := (*t.CompletedAt)[:10]
			if grouped[date] == nil {
				grouped[date] = map[int]HistoryItem{}
			}
			grouped[date][t.ID] = t
		}

		for _, pe := range progressLogs {
			date := pe.LogDate
			tid := pe.TodoID
			if grouped[date] == nil {
				grouped[date] = map[int]HistoryItem{}
			}
			if existing, ok := grouped[date][tid]; ok && existing.Type == "completed" {
				existing.Progress = pe.Progress
				grouped[date][tid] = existing
				continue
			}

			item := HistoryItem{
				ID:       tid,
				Title:    pe.Title,
				TaskType: pe.TaskType,
				Progress: pe.Progress,
			}
			if pe.ParentID.Valid {
				v := int(pe.ParentID.Int64)
				item.ParentID = &v
			}
			if pe.DueDate.Valid {
				item.DueDate = &pe.DueDate.String
			}
			if pe.StartDate.Valid {
				item.StartDate = &pe.StartDate.String
			}
			if pe.Completed == 1 && pe.CompletedAt.Valid {
				item.CompletedAt = &pe.CompletedAt.String
				item.Type = "completed"
			} else {
				item.Type = "progress"
			}
			grouped[date][tid] = item
		}

		result := map[string][]HistoryItem{}
		for date, items := range grouped {
			for _, item := range items {
				result[date] = append(result[date], item)
			}
		}

		type Stat struct {
			Date  string `json:"date"`
			Count int    `json:"count"`
		}
		var stats []Stat
		for date, items := range result {
			stats = append(stats, Stat{Date: date, Count: len(items)})
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"history": result,
			"stats":   stats,
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"history": map[string][]HistoryItem{},
		"stats":   []interface{}{},
	})
}

// ---------------------------------------------------------------------------
// - Timeline
// ---------------------------------------------------------------------------

var dateRepl = strings.NewReplacer("-", "", " ", "")

func isValidDate(s string) bool {
	if len(s) != 10 {
		return false
	}
	for i, c := range s {
		if i == 4 || i == 7 {
			if c != '-' {
				return false
			}
		} else if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func handleGetTimeline(w http.ResponseWriter, r *http.Request) {
	date := r.URL.Query().Get("date")
	if !isValidDate(date) {
		jsonError(w, "日期格式无效", http.StatusBadRequest)
		return
	}

	rows, err := db.Query("SELECT todo_id, slot_hour FROM timeline_slots WHERE slot_date = ? ORDER BY slot_hour ASC, id ASC", date)
	if err != nil {
		jsonError(w, "查询失败", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	schedule := map[string][]string{}
	for rows.Next() {
		var todoID, hour int
		if err := rows.Scan(&todoID, &hour); err != nil {
			continue
		}
		h := strconv.Itoa(hour)
		schedule[h] = append(schedule[h], strconv.Itoa(todoID))
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"schedule": schedule,
	})
}

func handleSaveTimeline(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		jsonError(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}

	action := r.FormValue("action")
	todoID, _ := strconv.Atoi(r.FormValue("todo_id"))
	date := r.FormValue("slot_date")
	hour, _ := strconv.Atoi(r.FormValue("slot_hour"))

	if action != "add" && action != "remove" {
		jsonError(w, "无效操作", http.StatusBadRequest)
		return
	}
	if todoID <= 0 {
		jsonError(w, "任务ID无效", http.StatusBadRequest)
		return
	}
	if !isValidDate(date) {
		jsonError(w, "日期格式无效", http.StatusBadRequest)
		return
	}
	if hour < 0 || hour > 23 {
		jsonError(w, "时间段无效", http.StatusBadRequest)
		return
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM todos WHERE id = ?", todoID).Scan(&count); err != nil {
		jsonError(w, "查询任务失败", http.StatusInternalServerError)
		return
	}
	if count == 0 {
		jsonError(w, "任务不存在", http.StatusBadRequest)
		return
	}

	if action == "add" {
		db.Exec("INSERT OR IGNORE INTO timeline_slots (todo_id, slot_date, slot_hour) VALUES (?, ?, ?)",
			todoID, date, hour)
	} else {
		db.Exec("DELETE FROM timeline_slots WHERE todo_id = ? AND slot_date = ? AND slot_hour = ?",
			todoID, date, hour)
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
	})
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

// ---------------------------------------------------------------------------
// - 打开浏览器
// ---------------------------------------------------------------------------

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	cmd.Start()
}

// alertDialog 弹出系统对话框提示错误信息（跨平台）
func alertDialog(title, message string) {
	switch runtime.GOOS {
	case "windows":
		exec.Command("cmd", "/c", "msg", "*", "/time:0", message).Run()
	case "darwin":
		exec.Command("osascript", "-e", `display dialog "`+message+`" buttons {"OK"} default button 1`).Run()
	default:
		// Linux 无统一弹窗命令，仅记录日志
	}
}

// ---------------------------------------------------------------------------
// - 主入口
// ---------------------------------------------------------------------------

func main() {
	// 定时清理过期的 nonce，防内存泄漏
	go func() {
		for range time.Tick(10 * time.Minute) {
			mu.Lock()
			now := time.Now()
			for k, v := range nonceStore {
				if now.After(v) {
					delete(nonceStore, k)
				}
			}
			mu.Unlock()
		}
	}()

	dataDir := resolveDataDir()
	cfg.AppDir = dataDir
	cfg.DataDir = dataDir
	cfg.LogDir = dataDir

	os.MkdirAll(filepath.Join(dataDir, "data"), 0755)
	os.MkdirAll(filepath.Join(dataDir, "logs"), 0755)

	// .env 处理：首次运行创建默认配置；若 .env 消失但数据库已存在则生成随机密码
	envPath := filepath.Join(cfg.DataDir, ".env")
	if _, err := os.Stat(envPath); os.IsNotExist(err) {
		dbPath := filepath.Join(cfg.DataDir, "data", "todo.db")
		if _, dbErr := os.Stat(dbPath); dbErr == nil {
			// 数据库已存在但 .env 消失，生成随机密码防静默回退（.env 只存哈希，明文仅打印到日志一次）
			buf := make([]byte, 8)
			rand.Read(buf)
			newPass := hex.EncodeToString(buf)
			envContent := fmt.Sprintf(`# 星记 桌面版配置
# 注意：.env 文件曾缺失，密码已重新生成，请查看日志
AUTH_USER=%s
AUTH_PASS=%s
LISTEN_ADDR=127.0.0.1:18000
`, authUser, hashPassword(newPass))
			os.WriteFile(envPath, []byte(envContent), 0644)
			log.Printf("╔══════════════════════════════════╗")
			log.Printf("║ .env 缺失！已重新生成配置文件")
			log.Printf("║ 用户名: %s", authUser)
			log.Printf("║ 新密码: %s（仅此次显示，请尽快登录后修改）", newPass)
			log.Printf("╚══════════════════════════════════╝")
		} else {
			envContent := fmt.Sprintf(`# 星记 桌面版配置
# AUTH_PASS 存储的是密码的 SHA256 哈希，修改密码时直接填新密码明文即可，启动时会自动转换
AUTH_USER=%s
AUTH_PASS=%s
LISTEN_ADDR=127.0.0.1:18000
`, authUser, authPassHash)
			os.WriteFile(envPath, []byte(envContent), 0644)
			log.Println("已创建 .env 配置文件")
		}
	}

	loadEnv()

	rotateLog(filepath.Join(dataDir, "logs"))
	logFile, err := os.OpenFile(filepath.Join(dataDir, "logs", "server.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		log.SetOutput(logFile)
		defer logFile.Close()
	}

	if err := initDB(); err != nil {
		log.Fatalf("数据库初始化失败: %v", err)
	}
	log.Println("数据库初始化成功")

	mux := http.NewServeMux()

	// Auth 是公开端点（challenge 和 login 不需要认证）
	mux.HandleFunc("/api/auth", handleAuth)

	// 需要认证的 API
	authAPIs := map[string]http.HandlerFunc{
		"/api/get_todos":       handleGetTodos,
		"/api/get_history":     handleGetHistory,
		"/api/add_todo":        handleAddTodo,
		"/api/update_todo":     handleUpdateTodo,
		"/api/complete_todo":   handleCompleteTodo,
		"/api/delete_todo":     handleDeleteTodo,
		"/api/reorder_todos":   handleReorderTodos,
		"/api/get_timeline":    handleGetTimeline,
		"/api/save_timeline":   handleSaveTimeline,
		"/api/export_csv":      handleExportCSV,
		"/api/export_json":     handleExportJSON,
		"/api/import_json":     handleImportJSON,
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

	listener, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		msg := fmt.Sprintf("星记启动失败：端口 %s 被占用，请检查是否有其他实例正在运行。", cfg.ListenAddr)
		log.Println(msg)
		alertDialog("星记", msg)
		os.Exit(1)
	}

	actualAddr := listener.Addr().String()
	log.Printf("星记服务启动于 http://%s", actualAddr)

	// HTTP server 在后台 goroutine 运行
	go func() {
		if err := http.Serve(listener, mux); err != nil {
			log.Fatalf("服务错误: %v", err)
		}
	}()

	// 打开浏览器
	if cfg.OpenBrowser {
		time.Sleep(500 * time.Millisecond)
		openBrowser("http://" + actualAddr)
	}

	// 系统托盘（阻塞，直到用户选择退出）
	startTray(actualAddr)
}