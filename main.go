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
	if !allowedTaskTypes[taskType] {
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
		if err := db.QueryRow("SELECT COUNT(*) FROM todos WHERE id = ?", *parentID).Scan(&count); err != nil {
			jsonError(w, "查询父任务失败", http.StatusInternalServerError)
			return
		}
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

	var setClauses []string
	var args []interface{}

	if data.Title != nil {
		setClauses = append(setClauses, "title = ?")
		args = append(args, *data.Title)
	}
	if data.TaskType != nil && allowedTaskTypes[*data.TaskType] {
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
	tx, err := db.Begin()
	if err != nil {
		jsonError(w, "事务启动失败", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	if _, err := tx.Exec("UPDATE todos SET "+strings.Join(setClauses, ", ")+" WHERE id = ?", args...); err != nil {
		jsonError(w, "更新失败", http.StatusBadRequest)
		return
	}

	if data.Progress != nil {
		today := time.Now().Format("2006-01-02")
		if _, err := tx.Exec(`INSERT INTO progress_log (todo_id, log_date, progress) VALUES (?, ?, ?)
			ON CONFLICT(todo_id, log_date) DO UPDATE SET progress = excluded.progress`,
			data.ID, today, *data.Progress); err != nil {
			jsonError(w, "记录进度失败", http.StatusInternalServerError)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		jsonError(w, "事务提交失败", http.StatusInternalServerError)
		return
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

	tx, err := db.Begin()
	if err != nil {
		jsonError(w, "事务启动失败", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	var completed int
	var parentID sql.NullInt64
	if err := tx.QueryRow("SELECT completed, parent_id FROM todos WHERE id = ?", id).Scan(&completed, &parentID); err != nil {
		if err == sql.ErrNoRows {
			jsonError(w, "任务不存在", http.StatusBadRequest)
		} else {
			jsonError(w, "查询任务失败", http.StatusInternalServerError)
		}
		return
	}

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

	if _, err := tx.Exec("UPDATE todos SET completed = ?, completed_at = ?, progress = ? WHERE id = ?",
		newStatus, completedAt, progressVal, id); err != nil {
		jsonError(w, "更新任务状态失败", http.StatusInternalServerError)
		return
	}

	logDate := completedDate
	if newStatus == 0 {
		logDate = time.Now().Format("2006-01-02")
	}
	if _, err := tx.Exec(`INSERT INTO progress_log (todo_id, log_date, progress) VALUES (?, ?, ?)
		ON CONFLICT(todo_id, log_date) DO UPDATE SET progress = excluded.progress`,
		id, logDate, progressVal); err != nil {
		jsonError(w, "记录进度日志失败", http.StatusInternalServerError)
		return
	}

	completedIDs := []int{id}

	if newStatus == 1 && completeChildren {
		rows, err := tx.Query("SELECT id FROM todos WHERE parent_id = ? AND completed = 0", id)
		if err != nil {
			jsonError(w, "查询子任务失败", http.StatusInternalServerError)
			return
		}
		for rows.Next() {
			var cid int
			if err := rows.Scan(&cid); err != nil {
				rows.Close()
				jsonError(w, "读取子任务失败", http.StatusInternalServerError)
				return
			}
			if _, err := tx.Exec("UPDATE todos SET completed = 1, completed_at = ?, progress = 100 WHERE id = ?", completedAt, cid); err != nil {
				rows.Close()
				jsonError(w, "更新子任务失败", http.StatusInternalServerError)
				return
			}
			if _, err := tx.Exec(`INSERT INTO progress_log (todo_id, log_date, progress) VALUES (?, ?, ?)
				ON CONFLICT(todo_id, log_date) DO UPDATE SET progress = excluded.progress`,
				cid, logDate, 100); err != nil {
				rows.Close()
				jsonError(w, "记录子任务进度失败", http.StatusInternalServerError)
				return
			}
			completedIDs = append(completedIDs, cid)
		}
		rows.Close()
	}

	if newStatus == 1 && parentID.Valid {
		pid := int(parentID.Int64)
		var total int
		var doneSum sql.NullInt64
		if err := tx.QueryRow("SELECT COUNT(*), SUM(completed) FROM todos WHERE parent_id = ?", pid).Scan(&total, &doneSum); err != nil {
			jsonError(w, "查询父任务子项失败", http.StatusInternalServerError)
			return
		}
		done := 0
		if doneSum.Valid {
			done = int(doneSum.Int64)
		}
		parentProgress := 0
		if total > 0 {
			parentProgress = (done / total) * 100
		}
		if _, err := tx.Exec(`INSERT INTO progress_log (todo_id, log_date, progress) VALUES (?, ?, ?)
			ON CONFLICT(todo_id, log_date) DO UPDATE SET progress = excluded.progress`,
			pid, logDate, parentProgress); err != nil {
			jsonError(w, "记录父任务进度失败", http.StatusInternalServerError)
			return
		}
		if total > 0 && total == done {
			if _, err := tx.Exec("UPDATE todos SET completed = 1, completed_at = ?, progress = 100 WHERE id = ?",
				completedDate+" "+time.Now().Format("15:04:05"), pid); err != nil {
				jsonError(w, "更新父任务状态失败", http.StatusInternalServerError)
				return
			}
			completedIDs = append(completedIDs, pid)
		}
	}

	if err := tx.Commit(); err != nil {
		jsonError(w, "事务提交失败", http.StatusInternalServerError)
		return
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
		if err := rows.Scan(&id, &title, &taskType, &parentID, &dueDate, &startDate, &completedAt, &progress); err != nil {
			continue
		}

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
// - JSON 全量导出
// ---------------------------------------------------------------------------

func handleExportJSON(w http.ResponseWriter, r *http.Request) {
	type todoRow struct {
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
	}
	type progressRow struct {
		ID        int    `json:"id"`
		TodoID    int    `json:"todo_id"`
		LogDate   string `json:"log_date"`
		Progress  int    `json:"progress"`
		CreatedAt string `json:"created_at"`
	}
	type timelineRow struct {
		ID        int    `json:"id"`
		TodoID    int    `json:"todo_id"`
		SlotDate  string `json:"slot_date"`
		SlotHour  int    `json:"slot_hour"`
		CreatedAt string `json:"created_at"`
	}

	backup := struct {
		Version      string        `json:"version"`
		ExportedAt   string        `json:"exported_at"`
		Todos        []todoRow     `json:"todos"`
		ProgressLogs []progressRow `json:"progress_logs"`
		Timeline     []timelineRow `json:"timeline"`
	}{
		Version:    "1.0",
		ExportedAt: time.Now().Format("2006-01-02 15:04:05"),
	}

	rows, err := db.Query(`SELECT id, title, task_type, parent_id, sort_order, progress, start_date, due_date, completed, completed_at, created_at FROM todos`)
	if err != nil {
		jsonError(w, "导出失败", http.StatusInternalServerError)
		return
	}
	for rows.Next() {
		var t todoRow
		var pid sql.NullInt64
		var pg sql.NullInt64
		var sd, dd, ca sql.NullString
		if err := rows.Scan(&t.ID, &t.Title, &t.TaskType, &pid, &t.SortOrder, &pg, &sd, &dd, &t.Completed, &ca, &t.CreatedAt); err != nil {
			rows.Close()
			jsonError(w, "导出失败", http.StatusInternalServerError)
			return
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
		backup.Todos = append(backup.Todos, t)
	}
	rows.Close()

	rows, err = db.Query(`SELECT id, todo_id, log_date, progress, created_at FROM progress_log`)
	if err != nil {
		jsonError(w, "导出失败", http.StatusInternalServerError)
		return
	}
	for rows.Next() {
		var p progressRow
		if err := rows.Scan(&p.ID, &p.TodoID, &p.LogDate, &p.Progress, &p.CreatedAt); err != nil {
			rows.Close()
			jsonError(w, "导出失败", http.StatusInternalServerError)
			return
		}
		backup.ProgressLogs = append(backup.ProgressLogs, p)
	}
	rows.Close()

	rows, err = db.Query(`SELECT id, todo_id, slot_date, slot_hour, created_at FROM timeline_slots`)
	if err != nil {
		jsonError(w, "导出失败", http.StatusInternalServerError)
		return
	}
	for rows.Next() {
		var s timelineRow
		if err := rows.Scan(&s.ID, &s.TodoID, &s.SlotDate, &s.SlotHour, &s.CreatedAt); err != nil {
			rows.Close()
			jsonError(w, "导出失败", http.StatusInternalServerError)
			return
		}
		backup.Timeline = append(backup.Timeline, s)
	}
	rows.Close()

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="star-track-backup-%s.json"`, time.Now().Format("20060102-150405")))
	json.NewEncoder(w).Encode(backup)
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
			// 数据库已存在但 .env 消失，生成随机密码防静默回退
			buf := make([]byte, 8)
			rand.Read(buf)
			authPass = hex.EncodeToString(buf)
			envContent := fmt.Sprintf(`# 星记 桌面版配置
# 注意：.env 文件曾缺失，密码已重新生成，请查看日志
AUTH_USER=%s
AUTH_PASS=%s
LISTEN_ADDR=127.0.0.1:18000
`, authUser, authPass)
			os.WriteFile(envPath, []byte(envContent), 0644)
			log.Printf("╔══════════════════════════════════╗")
			log.Printf("║ .env 缺失！已重新生成配置文件")
			log.Printf("║ 用户名: %s", authUser)
			log.Printf("║ 新密码: %s", authPass)
			log.Printf("╚══════════════════════════════════╝")
		} else {
			envContent := fmt.Sprintf(`# 星记 桌面版配置
AUTH_USER=%s
AUTH_PASS=%s
LISTEN_ADDR=127.0.0.1:18000
`, authUser, authPass)
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