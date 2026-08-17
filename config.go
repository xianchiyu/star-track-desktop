package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// 配置
// ---------------------------------------------------------------------------

type Config struct {
	ListenAddr  string
	DataDir     string
	LogDir      string
	OpenBrowser bool
	AppDir      string // exe 所在目录
}

var cfg = Config{
	ListenAddr:  "127.0.0.1:18000",
	OpenBrowser: true,
}

// ---------------------------------------------------------------------------
// 数据目录
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

// ---------------------------------------------------------------------------
// 日志轮转
// ---------------------------------------------------------------------------

// rotateLog 若 server.log 超过 maxLogSize，将其归档为 server-时间戳.log 并新建空日志；
// 同时清理旧的 server-*.log 归档，仅保留最近 keepCount 份。
func rotateLog(logDir string) {
	const maxLogSize = 5 << 20 // 5 MB
	const keepCount = 5        // 最多保留的归档份数

	logPath := filepath.Join(logDir, "server.log")
	if info, err := os.Stat(logPath); err == nil && info.Size() >= maxLogSize {
		stamp := time.Now().Format("20060102-150405")
		archive := filepath.Join(logDir, "server-"+stamp+".log")
		_ = os.Rename(logPath, archive)
	}

	// 清理旧归档，保留最近 keepCount 份
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return
	}
	var archives []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "server-") && strings.HasSuffix(e.Name(), ".log") {
			archives = append(archives, filepath.Join(logDir, e.Name()))
		}
	}
	// 按新建时间倒序，删除超出 keepCount 的旧文件
	sort.Slice(archives, func(i, j int) bool {
		a, _ := os.Stat(archives[i])
		b, _ := os.Stat(archives[j])
		return a.ModTime().After(b.ModTime())
	})
	if len(archives) > keepCount {
		for _, p := range archives[keepCount:] {
			_ = os.Remove(p)
		}
	}
}
