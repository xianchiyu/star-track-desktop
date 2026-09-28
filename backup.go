package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"
)

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
// - JSON 导入（覆盖式：清空后写入）
// ---------------------------------------------------------------------------

func handleImportJSON(w http.ResponseWriter, r *http.Request) {
	// 限制上传大小 10MB
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		jsonError(w, "文件过大或格式错误（上限 10MB）", http.StatusBadRequest)
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		jsonError(w, "未找到上传文件", http.StatusBadRequest)
		return
	}
	defer file.Close()

	var backup struct {
		Version      string `json:"version"`
		ExportedAt   string `json:"exported_at"`
		Todos        []struct {
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
		} `json:"todos"`
		ProgressLogs []struct {
			ID        int    `json:"id"`
			TodoID    int    `json:"todo_id"`
			LogDate   string `json:"log_date"`
			Progress  int    `json:"progress"`
			CreatedAt string `json:"created_at"`
		} `json:"progress_logs"`
		Timeline []struct {
			ID        int    `json:"id"`
			TodoID    int    `json:"todo_id"`
			SlotDate  string `json:"slot_date"`
			SlotHour  int    `json:"slot_hour"`
			CreatedAt string `json:"created_at"`
		} `json:"timeline"`
	}
	if err := json.NewDecoder(file).Decode(&backup); err != nil {
		jsonError(w, "JSON 解析失败，请确认是星记导出的备份文件", http.StatusBadRequest)
		return
	}
	if backup.Version == "" || len(backup.Todos) == 0 {
		jsonError(w, "备份文件结构不完整", http.StatusBadRequest)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		jsonError(w, "开始事务失败", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	// 清空三张表
	for _, table := range []string{"timeline_slots", "progress_log", "todos"} {
		if _, err := tx.Exec("DELETE FROM " + table); err != nil {
			jsonError(w, "清空 "+table+" 失败", http.StatusInternalServerError)
			return
		}
	}
	// 重置自增序列
	tx.Exec("DELETE FROM sqlite_sequence WHERE name IN ('todos','progress_log','timeline_slots')")

	// 按 ID 升序插入 todos，保证父任务先于子任务
	sort.Slice(backup.Todos, func(i, j int) bool { return backup.Todos[i].ID < backup.Todos[j].ID })
	for _, t := range backup.Todos {
		if _, err := tx.Exec(`INSERT INTO todos (id, title, task_type, parent_id, sort_order, progress, start_date, due_date, completed, completed_at, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			t.ID, t.Title, t.TaskType, t.ParentID, t.SortOrder, t.Progress, t.StartDate, t.DueDate, t.Completed, t.CompletedAt, t.CreatedAt); err != nil {
			jsonError(w, "写入 todos 失败（id="+strconv.Itoa(t.ID)+"）", http.StatusInternalServerError)
			return
		}
	}
	for _, p := range backup.ProgressLogs {
		if _, err := tx.Exec(`INSERT INTO progress_log (id, todo_id, log_date, progress, created_at) VALUES (?, ?, ?, ?, ?)`,
			p.ID, p.TodoID, p.LogDate, p.Progress, p.CreatedAt); err != nil {
			jsonError(w, "写入 progress_log 失败", http.StatusInternalServerError)
			return
		}
	}
	for _, s := range backup.Timeline {
		if _, err := tx.Exec(`INSERT INTO timeline_slots (id, todo_id, slot_date, slot_hour, created_at) VALUES (?, ?, ?, ?, ?)`,
			s.ID, s.TodoID, s.SlotDate, s.SlotHour, s.CreatedAt); err != nil {
			jsonError(w, "写入 timeline_slots 失败", http.StatusInternalServerError)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		jsonError(w, "提交事务失败", http.StatusInternalServerError)
		return
	}

	jsonSuccess(w, map[string]interface{}{
		"todos":    len(backup.Todos),
		"progress": len(backup.ProgressLogs),
		"timeline": len(backup.Timeline),
	})
}
