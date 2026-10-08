package main

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// ---------------------------------------------------------------------------
// - Timeline
// ---------------------------------------------------------------------------

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
