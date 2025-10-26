package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

// -------------------- 数据结构 --------------------

type AddTaskReq struct {
	IntervalSeconds int    `json:"interval_seconds" binding:"required,min=1,max=86400"`
	URL             string `json:"url"              binding:"required,url"`
	Method          string `json:"method"           binding:"required"`
	Description     string `json:"description"      binding:"required,min=1,max=200"`
}

type Task struct {
	ID              string
	IntervalSeconds int
	URL             string
	Method          string
	Description     string
	paused          bool
	startedAt       time.Time
	runCount        uint64
	cancel          context.CancelFunc
}

type persistedTask struct {
	ID              string `json:"id"`
	IntervalSeconds int    `json:"interval_seconds"`
	URL             string `json:"url"`
	Method          string `json:"method"`
	Description     string `json:"description"`
	Enabled         bool   `json:"enabled"`
	CreatedAt       string `json:"created_at,omitempty"`
	UpdatedAt       string `json:"updated_at,omitempty"`
}

type persistedFile struct {
	Version int             `json:"version"`
	Port    int             `json:"port"`
	Tasks   []persistedTask `json:"tasks"`
}

// -------------------- 调度器 --------------------

type Scheduler struct {
	mu       sync.RWMutex
	tasks    map[string]*Task
	client   *http.Client
	idSeq    uint64
	filePath string
	port     int
}

func NewScheduler(filePath string) *Scheduler {
	return &Scheduler{
		tasks:    make(map[string]*Task),
		client:   &http.Client{Timeout: 10 * time.Second},
		filePath: filePath,
		port:     9000, // 默认端口
	}
}

// 生成较短的有序ID（保留原逻辑：时间戳+随机+序号）
func (s *Scheduler) nextID() string {
	n := atomic.AddUint64(&s.idSeq, 1)
	now := time.Now().UTC().Format("20060102T150405.000000000")
	r := make([]byte, 2)
	if _, err := rand.Read(r); err != nil {
		return fmt.Sprintf("%s-%d", now, n)
	}
	return fmt.Sprintf("%s-%02x%02x-%d", now, r[0], r[1], n)
}

// -------------------- 持久化 --------------------

func atomicWriteJSON(path string, v any) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *Scheduler) saveToDiskLocked() error {
	out := persistedFile{Version: 1, Port: s.port}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, t := range s.tasks {
		created := t.startedAt
		if created.IsZero() {
			created = time.Now().UTC()
		}
		out.Tasks = append(out.Tasks, persistedTask{
			ID:              t.ID,
			IntervalSeconds: t.IntervalSeconds,
			URL:             t.URL,
			Method:          t.Method,
			Description:     t.Description,
			Enabled:         !t.paused,
			CreatedAt:       created.Format(time.RFC3339Nano),
			UpdatedAt:       now,
		})
	}
	sort.Slice(out.Tasks, func(i, j int) bool { return out.Tasks[i].ID < out.Tasks[j].ID })
	return atomicWriteJSON(s.filePath, out)
}

func (s *Scheduler) loadFromDisk() error {
	// 不存在则创建默认配置
	if _, err := os.Stat(s.filePath); os.IsNotExist(err) {
		log.Printf("config not found, creating %s", s.filePath)
		def := persistedFile{Version: 1, Port: s.port, Tasks: []persistedTask{}}
		return atomicWriteJSON(s.filePath, def)
	}

	f, err := os.Open(s.filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	var pf persistedFile
	if err := json.NewDecoder(f).Decode(&pf); err != nil {
		return fmt.Errorf("invalid config.json: %v", err)
	}
	if pf.Port > 0 {
		s.port = pf.Port
	}
	log.Printf("using port: %d", s.port)

	// 恢复任务
	restored := 0
	for _, pt := range pf.Tasks {
		method := strings.ToUpper(strings.TrimSpace(pt.Method))
		if method != "GET" && method != "POST" {
			log.Printf("[restore] skip %s: invalid method=%q", pt.ID, pt.Method)
			continue
		}
		if pt.IntervalSeconds < 1 || strings.TrimSpace(pt.URL) == "" {
			log.Printf("[restore] skip %s: invalid interval/url", pt.ID)
			continue
		}
		ctx, cancel := context.WithCancel(context.Background())
		t := &Task{
			ID:              pt.ID,
			IntervalSeconds: pt.IntervalSeconds,
			URL:             pt.URL,
			Method:          method,
			Description:     pt.Description,
			paused:          !pt.Enabled,
			startedAt:       time.Now().UTC(),
		}
		s.tasks[t.ID] = t
		if pt.Enabled {
			t.cancel = cancel
			go s.runTask(ctx, t)
			log.Printf("[restore] RUNNING id=%s %s %s every %ds", t.ID, t.Method, t.URL, t.IntervalSeconds)
		} else {
			cancel()
			log.Printf("[restore] PAUSED  id=%s %s %s every %ds", t.ID, t.Method, t.URL, t.IntervalSeconds)
		}
		restored++
	}
	log.Printf("[restore] done: restored=%d, file=%s", restored, s.filePath)
	return nil
}

// -------------------- 任务操作 --------------------

func (s *Scheduler) AddTask(req AddTaskReq) (string, error) {
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method != "GET" && method != "POST" {
		return "", errors.New("method must be GET or POST")
	}
	if req.IntervalSeconds < 1 {
		return "", errors.New("interval_seconds must be >= 1")
	}

	id := s.nextID()
	s.mu.Lock()
	defer s.mu.Unlock()

	t := &Task{
		ID:              id,
		IntervalSeconds: req.IntervalSeconds,
		URL:             req.URL,
		Method:          method,
		Description:     strings.TrimSpace(req.Description),
		paused:          false,
		startedAt:       time.Now().UTC(),
	}
	s.tasks[id] = t

	if err := s.saveToDiskLocked(); err != nil {
		delete(s.tasks, id)
		return "", err
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.cancel = cancel
	go s.runTask(ctx, t)
	return id, nil
}

func (s *Scheduler) RemoveTask(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return false
	}
	if t.cancel != nil {
		t.cancel()
		t.cancel = nil
	}
	delete(s.tasks, id)
	_ = s.saveToDiskLocked()
	return true
}

func (s *Scheduler) PauseTask(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return false
	}
	if t.paused {
		return true
	}
	if t.cancel != nil {
		t.cancel()
		t.cancel = nil
	}
	t.paused = true
	_ = s.saveToDiskLocked()
	return true
}

// -------------------- 执行循环 --------------------

func (s *Scheduler) runTask(ctx context.Context, t *Task) {
	// 立即执行一次（如不需要可注释）
	s.executeOnce(t)

	ticker := time.NewTicker(time.Duration(t.IntervalSeconds) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("[task %s] stopped", t.ID)
			return
		case <-ticker.C:
			s.executeOnce(t)
		}
	}
}

func (s *Scheduler) executeOnce(t *Task) {
	req, err := http.NewRequest(t.Method, t.URL, nil) // POST 无 body
	if err != nil {
		log.Printf("[task %s] build request error: %v", t.ID, err)
		return
	}
	req.Header.Set("User-Agent", "gin-scheduler/1.7")
	resp, err := s.client.Do(req)
	if err != nil {
		log.Printf("[task %s] request error: %v", t.ID, err)
		return
	}
	if resp.Body != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
	atomic.AddUint64(&t.runCount, 1)
	log.Printf("[task %s] %s %s -> %s (run=%d)", t.ID, t.Method, t.URL, resp.Status, atomic.LoadUint64(&t.runCount))
}

// -------------------- 入口 & 路由 --------------------

func main() {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	// config.json 固定在当前工作目录
	wd, _ := os.Getwd()
	cfgPath := filepath.Join(wd, "config.json")
	s := NewScheduler(cfgPath)
	if err := s.loadFromDisk(); err != nil {
		log.Printf("load error: %v", err)
	}

	// —— 查询：唯一使用 GET ——
	r.GET("/tasks", func(c *gin.Context) {
		s.mu.RLock()
		defer s.mu.RUnlock()
		type view struct {
			ID              string `json:"id"`
			IntervalSeconds int    `json:"interval_seconds"`
			URL             string `json:"url"`
			Method          string `json:"method"`
			Description     string `json:"description"`
			Status          string `json:"status"`
		}
		var out []view
		for _, t := range s.tasks {
			status := "running"
			if t.paused {
				status = "paused"
			}
			out = append(out, view{
				ID:              t.ID,
				IntervalSeconds: t.IntervalSeconds,
				URL:             t.URL,
				Method:          t.Method,
				Description:     t.Description,
				Status:          status,
			})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		c.JSON(http.StatusOK, gin.H{"tasks": out})
	})

	r.GET("/tasks/:id", func(c *gin.Context) {
		id := c.Param("id")
		s.mu.RLock()
		t, ok := s.tasks[id]
		s.mu.RUnlock()
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
			return
		}
		status := "running"
		if t.paused {
			status = "paused"
		}
		c.JSON(http.StatusOK, gin.H{
			"id":               t.ID,
			"interval_seconds": t.IntervalSeconds,
			"url":              t.URL,
			"method":           t.Method,
			"description":      t.Description,
			"status":           status,
		})
	})

	// 添加任务（唯一需要 JSON 请求体）
	r.POST("/tasks/add", func(c *gin.Context) {
		var req AddTaskReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		id, err := s.AddTask(req)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"id": id})
	})

	// 暂停任务（无请求体）
	r.POST("/tasks/:id/pause", func(c *gin.Context) {
		id := c.Param("id")
		if !s.PauseTask(id) {
			c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	// 删除任务（无请求体）
	r.POST("/tasks/:id/delete", func(c *gin.Context) {
		id := c.Param("id")
		if !s.RemoveTask(id) {
			c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	// 健康检查：支持 POST（保留 GET 兼容）
	r.POST("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })

	log.Printf("listening on :%d (from config.json)", s.port)
	if err := r.Run(fmt.Sprintf(":%d", s.port)); err != nil {
		log.Fatal(err)
	}
}
