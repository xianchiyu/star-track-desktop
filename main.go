package main

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
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

var (
	db         *sql.DB
	authUser   = "pilot"
	authPass   = "startrack"
	secretKey  string
	jwtSecret  []byte
	mu         sync.Mutex
	nonceStore = map[string]time.Time{}
)

type Config struct {
	ListenAddr  string
	DataDir     string
	LogDir      string
	OpenBrowser bool
	AppDir      string // exe 所在目录
}

var cfg = Config{
	ListenAddr:  "127.0.0.1:8000",
	OpenBrowser: true,
}

// ---------------------------------------------------------------------------
// 工具函数
// ---------------------------------------------------------------------------

func init() {
	buf := make([]byte, 32)
	rand.Read(buf)
	secretKey = hex.EncodeToString(buf)
	jwtSecret = []byte(secretKey)
}

func loadEnv() {
	data, err := os.ReadFile(filepath.Join(cfg.DataDir, ".env"))
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
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
				authPass = v
			case "LISTEN_ADDR":
				cfg.ListenAddr = v
			}
		}
	}
}

func base64URLEncode(src []byte) string {
	return hex.EncodeToString(src)
}

func createToken(username string) (string, error) {
	header := `{"alg":"HS256","typ":"JWT"}`
	now := time.Now().Unix()
	payload := fmt.Sprintf(`{"sub":"%s","iat":%d,"exp":%d}`, username, now, now+86400*7)

	b64 := func(b []byte) string {
		return strings.TrimRight(base64URLEncode(b), "=")
	}

	headerB64 := b64([]byte(header))
	payloadB64 := b64([]byte(payload))

	sig := sha256.Sum256([]byte(headerB64 + "." + payloadB64 + string(jwtSecret)))
	sigB64 := b64(sig[:])

	return headerB64 + "." + payloadB64 + "." + sigB64, nil
}

func verifyToken(token string) (string, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", false
	}
	sig := sha256.Sum256([]byte(parts[0] + "." + parts[1] + string(jwtSecret)))
	expected := base64URLEncode(sig[:])
	if parts[2] != expected {
		return "", false
	}
	payloadHex := parts[1]
	payloadBytes, err := hex.DecodeString(payloadHex)
	if err != nil {
		return "", false
	}
	var claims struct {
		Sub string `json:"sub"`
		Exp int64  `json:"exp"`
	}
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return "", false
	}
	if time.Now().Unix() > claims.Exp {
		return "", false
	}
	return claims.Sub, true
}

func extractToken(r *http.Request) string {
	t := r.Header.Get("X-CSRF-Token")
	if t != "" {
		return t
	}
	t = r.URL.Query().Get("token")
	if t != "" {
		return t
	}
	// 从 form body 取（auth.php 的 logout）
	t = r.FormValue("csrf_token")
	return t
}

func requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			token := extractToken(r)
			user, ok := verifyToken(token)
			if !ok {
				http.Error(w, `{"error":"未登录"}`, http.StatusUnauthorized)
				return
			}
			r.Header.Set("X-Auth-User", user)
		}
		next(w, r)
	}
}

func requireCsrf(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" || r.Method == "PUT" || r.Method == "DELETE" {
			token := extractToken(r)
			_, ok := verifyToken(token)
			if !ok {
				http.Error(w, `{"error":"CSRF 校验失败"}`, http.StatusForbidden)
				return
			}
		}
		next(w, r)
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
// - Auth 处理器（公开端点 = 不需要认证）
// ---------------------------------------------------------------------------

func handleAuth(w http.ResponseWriter, r *http.Request) {
	action := r.FormValue("action")
	if action == "" {
		action = r.URL.Query().Get("action")
	}

	switch action {
	case "challenge":
		mu.Lock()
		nonce := make([]byte, 16)
		rand.Read(nonce)
		nonceStr := hex.EncodeToString(nonce)
		nonceStore[nonceStr] = time.Now().Add(5 * time.Minute)
		mu.Unlock()
		json.NewEncoder(w).Encode(map[string]string{"nonce": nonceStr})

	case "login":
		user := strings.TrimSpace(r.FormValue("username"))
		clientHash := strings.ToLower(strings.TrimSpace(r.FormValue("hash")))
		nonce := r.FormValue("nonce")

		mu.Lock()
		_, nonceOK := nonceStore[nonce]
		if nonce != "" && nonceOK {
			delete(nonceStore, nonce)
		}
		mu.Unlock()

		if nonce != "" && !nonceOK {
			jsonError(w, "验证令牌无效或已过期", http.StatusBadRequest)
			return
		}

		expected := sha256.Sum256([]byte(nonce + authPass))
		expectedHash := hex.EncodeToString(expected[:])

		if user == authUser && clientHash == expectedHash {
			token, err := createToken(user)
			if err != nil {
				jsonError(w, "生成令牌失败", http.StatusInternalServerError)
				return
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success":    true,
				"csrf_token": token,
			})
		} else {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "用户名或密码不对",
			})
		}

	case "logout":
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
		})

	case "check":
		token := extractToken(r)
		_, ok := verifyToken(token)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"logged_in": ok,
		})

	default:
		jsonError(w, "未知操作", http.StatusBadRequest)
	}
}

// ---------------------------------------------------------------------------
// - Todos CRUD
// ---------------------------------------------------------------------------

type Todo struct {
	ID          int     `json:"id"`
	Title       string  `json:"title"`
	TaskType    string  `json:"task_type"`
	ParentID    *int    `json:"parent_id"`
	SortOrder   int     `json:"sort_order"`
	Progress    *int    `json:"progress"`
	StartDate   *string `json:"start_date"`
	DueDate     *string `json:"due_date"`
	Completed   int     `json:"completed"`
	CompletedAt *string `json:"completed_at"`
	CreatedAt   string  `json:"created_at"`
	Children    []Todo  `json:"children"`
}

func handleGetTodos(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`SELECT id, title, task_type, parent_id, sort_order, progress, start_date, due_date, completed, completed_at, created_at FROM todos WHERE completed = 0 ORDER BY sort_order ASC, created_at ASC`)
	if err != nil {
		jsonError(w, "查询失败", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var allTodos []Todo
	for rows.Next() {
		var t Todo
		var pid sql.NullInt64
		var pg sql.NullInt64
		var sd, dd, ca sql.NullString
		err := rows.Scan(&t.ID, &t.Title, &t.TaskType, &pid, &t.SortOrder, &pg, &sd, &dd, &t.Completed, &ca, &t.CreatedAt)
		if err != nil {
			continue
		}
		if pid.Valid {
			v := int(pid.Int64)
			t.ParentID = &v
		}
		if pg.Valid {
			v := int(pg.Int64)
			t.Progress = &v
		}
		if sd.Valid {
			t.StartDate = &sd.String
		}
		if dd.Valid {
			t.DueDate = &dd.String
		}
		if ca.Valid {
			t.CompletedAt = &ca.String
		}
		allTodos = append(allTodos, t)
	}

	byParent := map[int][]Todo{}
	var roots []Todo
	for _, t := range allTodos {
		if t.ParentID == nil {
			roots = append(roots, t)
		} else {
			pid := *t.ParentID
			byParent[pid] = append(byParent[pid], t)
		}
	}

	var buildTree func(todo Todo) Todo
	buildTree = func(todo Todo) Todo {
		children := byParent[todo.ID]
		var builtChildren []Todo
		for _, c := range children {
			builtChildren = append(builtChildren, buildTree(c))
		}
		todo.Children = builtChildren

		if todo.Progress != nil {
		} else if len(builtChildren) > 0 {
			sum := 0
			for _, c := range builtChildren {
				if c.Progress != nil {
					sum += *c.Progress
				}
			}
			avg := sum / len(builtChildren)
			todo.Progress = &avg
		} else {
			v := 0
			if todo.Completed == 1 {
				v = 100
			}
			todo.Progress = &v
		}
		return todo
	}

	tree := make([]Todo, len(roots))
	for i, r := range roots {
		tree[i] = buildTree(r)
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"todos":   tree,
	})
}

func handleAddTodo(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		jsonError(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}

	title := r.FormValue("title")
	taskType := r.FormValue("task_type")
	if taskType == "" {
		taskType = "self"
	}
	allowedTypes := map[string]bool{"self": true, "family": true, "money": true, "sport": true, "love": true, "study": true}
	if !allowedTypes[taskType] {
		taskType = "self"
	}

	if title == "" {
		jsonError(w, "标题不能为空", http.StatusBadRequest)
		return
	}

	var parentID *int
	if pid := r.FormValue("parent_id"); pid != "" {
		if v, err := strconv.Atoi(pid); err == nil {
			parentID = &v
		}
	}

	var progress *int
	if pg := r.FormValue("progress"); pg != "" {
		if v, err := strconv.Atoi(pg); err == nil {
			progress = &v
		}
	}

	var startDate, dueDate *string
	if sd := r.FormValue("start_date"); sd != "" {
		startDate = &sd
	}
	if dd := r.FormValue("due_date"); dd != "" {
		dueDate = &dd
	}

	if parentID != nil {
		var count int
		db.QueryRow("SELECT COUNT(*) FROM todos WHERE id = ?", *parentID).Scan(&count)
		if count == 0 {
			jsonError(w, "父任务不存在", http.StatusBadRequest)
			return
		}
	}

	result, err := db.Exec(`INSERT INTO todos (title, task_type, parent_id, progress, start_date, due_date) VALUES (?, ?, ?, ?, ?, ?)`,
		title, taskType, parentID, progress, startDate, dueDate)
	if err != nil {
		jsonError(w, "添加任务失败", http.StatusBadRequest)
		return
	}

	id, _ := result.LastInsertId()
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"id":      int(id),
		"message": "任务添加成功",
	})
}

func handleUpdateTodo(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		jsonError(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}

	var data struct {
		ID        int     `json:"id"`
		Title     *string `json:"title"`
		TaskType  *string `json:"task_type"`
		Progress  *int    `json:"progress"`
		StartDate *string `json:"start_date"`
		DueDate   *string `json:"due_date"`
		ParentID  *int    `json:"parent_id"`
		SortOrder *int    `json:"sort_order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		jsonError(w, "请求格式错误", http.StatusBadRequest)
		return
	}

	if data.ID == 0 {
		jsonError(w, "缺少任务ID", http.StatusBadRequest)
		return
	}

	allowedTypes := map[string]bool{"self": true, "family": true, "money": true, "sport": true, "love": true, "study": true}

	var setClauses []string
	var args []interface{}

	if data.Title != nil {
		setClauses = append(setClauses, "title = ?")
		args = append(args, *data.Title)
	}
	if data.TaskType != nil && allowedTypes[*data.TaskType] {
		setClauses = append(setClauses, "task_type = ?")
		args = append(args, *data.TaskType)
	}
	if data.Progress != nil {
		setClauses = append(setClauses, "progress = ?")
		args = append(args, *data.Progress)
	}
	if data.StartDate != nil {
		setClauses = append(setClauses, "start_date = ?")
		args = append(args, *data.StartDate)
	}
	if data.DueDate != nil {
		setClauses = append(setClauses, "due_date = ?")
		args = append(args, *data.DueDate)
	}
	if data.ParentID != nil {
		setClauses = append(setClauses, "parent_id = ?")
		args = append(args, *data.ParentID)
	}
	if data.SortOrder != nil {
		setClauses = append(setClauses, "sort_order = ?")
		args = append(args, *data.SortOrder)
	}

	if len(setClauses) == 0 {
		jsonError(w, "没有需要更新的字段", http.StatusBadRequest)
		return
	}

	args = append(args, data.ID)
	_, err := db.Exec("UPDATE todos SET "+strings.Join(setClauses, ", ")+" WHERE id = ?", args...)
	if err != nil {
		jsonError(w, "更新失败", http.StatusBadRequest)
		return
	}

	if data.Progress != nil {
		today := time.Now().Format("2006-01-02")
		db.Exec(`INSERT INTO progress_log (todo_id, log_date, progress) VALUES (?, ?, ?)
			ON CONFLICT(todo_id, log_date) DO UPDATE SET progress = excluded.progress`,
			data.ID, today, *data.Progress)
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "任务更新成功",
	})
}

func handleCompleteTodo(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		jsonError(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}

	idStr := r.FormValue("id")
	completedDate := r.FormValue("completed_date")
	completeChildren := r.FormValue("complete_children") == "1"

	id, err := strconv.Atoi(idStr)
	if err != nil || id == 0 {
		jsonError(w, "任务ID无效", http.StatusBadRequest)
		return
	}

	if completedDate == "" {
		completedDate = time.Now().Format("2006-01-02")
	} else {
		if _, err := time.Parse("2006-01-02", completedDate); err != nil {
			jsonError(w, "日期格式无效", http.StatusBadRequest)
			return
		}
	}

	var completed int
	var parentID sql.NullInt64
	db.QueryRow("SELECT completed, parent_id FROM todos WHERE id = ?", id).Scan(&completed, &parentID)

	newStatus := 0
	if completed == 0 {
		newStatus = 1
	}
	completedAt := ""
	if newStatus == 1 {
		completedAt = completedDate + " " + time.Now().Format("15:04:05")
	}

	progressVal := 0
	if newStatus == 1 {
		progressVal = 100
	}

	db.Exec("UPDATE todos SET completed = ?, completed_at = ?, progress = ? WHERE id = ?",
		newStatus, completedAt, progressVal, id)

	logDate := completedDate
	if newStatus == 0 {
		logDate = time.Now().Format("2006-01-02")
	}
	db.Exec(`INSERT INTO progress_log (todo_id, log_date, progress) VALUES (?, ?, ?)
		ON CONFLICT(todo_id, log_date) DO UPDATE SET progress = excluded.progress`,
		id, logDate, progressVal)

	completedIDs := []int{id}

	if newStatus == 1 && completeChildren {
		rows, _ := db.Query("SELECT id FROM todos WHERE parent_id = ? AND completed = 0", id)
		if rows != nil {
			defer rows.Close()
			for rows.Next() {
				var cid int
				rows.Scan(&cid)
				db.Exec("UPDATE todos SET completed = 1, completed_at = ?, progress = 100 WHERE id = ?", completedAt, cid)
				db.Exec(`INSERT INTO progress_log (todo_id, log_date, progress) VALUES (?, ?, ?)
					ON CONFLICT(todo_id, log_date) DO UPDATE SET progress = excluded.progress`,
					cid, logDate, 100)
				completedIDs = append(completedIDs, cid)
			}
		}
	}

	if newStatus == 1 && parentID.Valid {
		pid := int(parentID.Int64)
		var total, done int
		db.QueryRow("SELECT COUNT(*), SUM(completed) FROM todos WHERE parent_id = ?", pid).Scan(&total, &done)
		parentProgress := 0
		if total > 0 {
			parentProgress = (done / total) * 100
		}
		db.Exec(`INSERT INTO progress_log (todo_id, log_date, progress) VALUES (?, ?, ?)
			ON CONFLICT(todo_id, log_date) DO UPDATE SET progress = excluded.progress`,
			pid, logDate, parentProgress)
		if total > 0 && total == done {
			db.Exec("UPDATE todos SET completed = 1, completed_at = ?, progress = 100 WHERE id = ?",
				completedDate+" "+time.Now().Format("15:04:05"), pid)
			completedIDs = append(completedIDs, pid)
		}
	}

	isCompleted := newStatus == 1
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":       true,
		"message":       "任务状态已更新",
		"completed":     isCompleted,
		"completed_ids": completedIDs,
	})
}

func handleDeleteTodo(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		jsonError(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}

	idStr := r.FormValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil || id == 0 {
		jsonError(w, "任务ID无效", http.StatusBadRequest)
		return
	}

	result, err := db.Exec("DELETE FROM todos WHERE id = ?", id)
	if err != nil {
		jsonError(w, "删除失败", http.StatusBadRequest)
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		jsonError(w, "任务不存在或已被删除", http.StatusBadRequest)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "任务已删除",
	})
}

func handleReorderTodos(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		jsonError(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}

	var data struct {
		ParentID *int  `json:"parent_id"`
		Order    []int `json:"order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		jsonError(w, "参数不完整", http.StatusBadRequest)
		return
	}

	if data.Order == nil {
		jsonError(w, "参数不完整", http.StatusBadRequest)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		jsonError(w, "事务失败", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	var stmt *sql.Stmt
	if data.ParentID == nil {
		stmt, _ = tx.Prepare("UPDATE todos SET sort_order = ? WHERE id = ? AND parent_id IS NULL")
	} else {
		stmt, _ = tx.Prepare("UPDATE todos SET sort_order = ? WHERE id = ? AND parent_id = ?")
	}
	if stmt != nil {
		defer stmt.Close()
		for idx, id := range data.Order {
			if data.ParentID == nil {
				stmt.Exec(idx, id)
			} else {
				stmt.Exec(idx, id, *data.ParentID)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		jsonError(w, "提交失败", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "排序已更新",
	})
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
		rows.Scan(&item.ID, &item.Title, &item.TaskType, &pid, &dd, &sd, &ca, &pg)
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
			logRows.Scan(&pe.TodoID, &pe.LogDate, &pe.Progress,
				&pe.Title, &pe.TaskType, &pe.ParentID, &pe.DueDate, &pe.StartDate, &pe.Completed, &pe.CompletedAt)
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
		rows.Scan(&todoID, &hour)
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
	db.QueryRow("SELECT COUNT(*) FROM todos WHERE id = ?", todoID).Scan(&count)
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
// - Export CSV
// ---------------------------------------------------------------------------

func handleExportCSV(w http.ResponseWriter, r *http.Request) {
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	if from == "" {
		from = time.Now().Format("2006-01")
	}
	if to == "" {
		to = time.Now().Format("2006-01")
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="star-track-%s-to-%s.csv"`, from, to))
	w.Write([]byte{0xEF, 0xBB, 0xBF})
	w.Write([]byte("完成日期,任务名称,类型,计划开始,计划截止,进度,完成时间\n"))

	startDate := from + "-01"
	endObj, _ := time.Parse("2006-01-02", to+"-01")
	endDate := endObj.AddDate(0, 1, 0).Format("2006-01-02")

	rows, err := db.Query(`
		SELECT id, title, task_type, parent_id, due_date, start_date, completed_at, progress
		FROM todos
		WHERE completed = 1 AND completed_at IS NOT NULL
		  AND completed_at >= ? AND completed_at < ?
		ORDER BY completed_at ASC
	`, startDate, endDate)
	if err != nil {
		jsonError(w, "导出失败", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	typeNames := map[string]string{
		"self":   "自我时间",
		"family": "家庭",
		"money":  "赚钱",
		"sport":  "运动",
		"love":   "爱情",
		"study":  "学习",
	}

	for rows.Next() {
		var id int
		var title, taskType string
		var parentID sql.NullInt64
		var dueDate, startDate, completedAt sql.NullString
		var progress sql.NullInt64
		rows.Scan(&id, &title, &taskType, &parentID, &dueDate, &startDate, &completedAt, &progress)

		completedDate := ""
		if completedAt.Valid && len(completedAt.String) >= 10 {
			completedDate = completedAt.String[:10]
		}

		doneAt := ""
		if completedAt.Valid && len(completedAt.String) > 10 {
			doneAt = completedAt.String[11:19]
		}

		pg := 0
		if progress.Valid {
			pg = int(progress.Int64)
		}

		t := taskType
		if name, ok := typeNames[taskType]; ok {
			t = name
		}

		sd := ""
		if startDate.Valid {
			sd = startDate.String
		}
		dd := ""
		if dueDate.Valid {
			dd = dueDate.String
		}

		fmt.Fprintf(w, "%s,\"%s\",%s,%s,%s,%d%%,%s\n",
			completedDate, title, t, sd, dd, pg, doneAt)
	}
}

// ---------------------------------------------------------------------------
// - 静态文件服务
// ---------------------------------------------------------------------------

func handlePage(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "/" || path == "/index.php" || path == "/index" {
		path = "/index.php"
	} else if path == "/login.php" || path == "/login" {
		path = "/login.php"
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
	if strings.HasSuffix(path, ".css") {
		contentType = "text/css; charset=utf-8"
	} else if strings.HasSuffix(path, ".js") {
		contentType = "application/javascript"
	} else if strings.HasSuffix(path, ".svg") {
		contentType = "image/svg+xml"
	} else if strings.HasSuffix(path, ".woff2") {
		contentType = "font/woff2"
	} else if strings.HasSuffix(path, ".woff") {
		contentType = "font/woff"
	} else if strings.HasSuffix(path, ".png") {
		contentType = "image/png"
	} else if strings.HasSuffix(path, ".jpg") || strings.HasSuffix(path, ".jpeg") {
		contentType = "image/jpeg"
	} else if strings.HasSuffix(path, ".gif") {
		contentType = "image/gif"
	} else if strings.HasSuffix(path, ".ico") {
		contentType = "image/x-icon"
	}

	w.Header().Set("Content-Type", contentType)

	if strings.HasSuffix(path, ".php") {
		content := string(data)
		content = removePHPTags(content)
		w.Write([]byte(content))
	} else {
		w.Write(data)
	}
}

func removePHPTags(s string) string {
	var result strings.Builder
	i := 0
	for i < len(s) {
		phpStart := strings.Index(s[i:], "<?php")
		if phpStart == -1 {
			result.WriteString(s[i:])
			break
		}
		result.WriteString(s[i : i+phpStart])
		phpEnd := strings.Index(s[i+phpStart:], "?>")
		if phpEnd == -1 {
			break
		}
		phpContent := s[i+phpStart : i+phpStart+phpEnd+2]
		if strings.Contains(phpContent, "getCsrfToken") || strings.Contains(phpContent, "csrf_token") {
			result.WriteString(`<meta name="csrf-token" content="">`)
		}
		i += phpStart + phpEnd + 2
	}
	return result.String()
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

// ---------------------------------------------------------------------------
// - 主入口
// ---------------------------------------------------------------------------

// resolveDataDir 决定数据目录：exe 同路径可写就用同路径（绿色版），
// 否则 fallback 到 %APPDATA%/星记（安装版，exe 在 Program Files 时走这条）
func resolveDataDir() string {
	exePath, err := os.Executable()
	if err == nil {
		exeDir := filepath.Dir(exePath)
		probe := filepath.Join(exeDir, ".write-probe")
		if f, err := os.Create(probe); err == nil {
			f.Close()
			os.Remove(probe)
			return exeDir
		}
	}
	appData, err := os.UserConfigDir()
	if err != nil {
		return "."
	}
	return filepath.Join(appData, "星记")
}

func main() {
	dataDir := resolveDataDir()
	cfg.AppDir = dataDir
	cfg.DataDir = dataDir
	cfg.LogDir = dataDir

	os.MkdirAll(filepath.Join(dataDir, "data"), 0755)
	os.MkdirAll(filepath.Join(dataDir, "logs"), 0755)

	loadEnv()

	logFile, err := os.OpenFile(filepath.Join(dataDir, "logs", "server.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		log.SetOutput(logFile)
		defer logFile.Close()
	}

	if err := initDB(); err != nil {
		log.Fatalf("数据库初始化失败: %v", err)
	}
	log.Println("数据库初始化成功")

	envPath := filepath.Join(cfg.DataDir, ".env")
	if _, err := os.Stat(envPath); os.IsNotExist(err) {
		envContent := fmt.Sprintf(`# 星记 桌面版配置
AUTH_USER=%s
AUTH_PASS=%s
LISTEN_ADDR=127.0.0.1:18000
`, authUser, authPass)
		os.WriteFile(envPath, []byte(envContent), 0644)
		log.Println("已创建 .env 配置文件")
	}

	mux := http.NewServeMux()

	// Auth 是公开端点（challenge 和 login 不需要认证）
	mux.HandleFunc("/api/auth.php", handleAuth)

	// 需要认证的 API
	authAPIs := map[string]http.HandlerFunc{
		"/api/get_todos.php":       handleGetTodos,
		"/api/get_history.php":     handleGetHistory,
		"/api/add_todo.php":        handleAddTodo,
		"/api/update_todo.php":     handleUpdateTodo,
		"/api/complete_todo.php":   handleCompleteTodo,
		"/api/delete_todo.php":     handleDeleteTodo,
		"/api/reorder_todos.php":   handleReorderTodos,
		"/api/get_timeline.php":    handleGetTimeline,
		"/api/save_timeline.php":   handleSaveTimeline,
		"/api/export_csv.php":      handleExportCSV,
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
		log.Fatalf("端口 %s 启动失败: %v", cfg.ListenAddr, err)
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