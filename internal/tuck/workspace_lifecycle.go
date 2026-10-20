package tuck

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type managedWorkspace struct {
	ID, Root, Path, Identity, State, LastError string
	CleanupAllowed                             bool
}

func (f *Files) useWorkspace(task M) (func(), error) {
	if f.Manager == nil {
		return func() {}, nil
	}
	return f.Manager.useWorkspace(task)
}

func (m *Manager) useTask(id string) (func(), error) {
	m.mu.Lock()
	defer m.unlock()
	if m.closed {
		return nil, Problem(409, "service_closing", "服务正在关闭")
	}
	if _, err := m.getLocked(id); err != nil {
		return nil, err
	}
	if m.taskUses == nil {
		m.taskUses = map[string]int{}
	}
	m.taskUses[id]++
	return func() {
		m.mu.Lock()
		defer m.unlock()
		m.taskUses[id]--
		if m.taskUses[id] == 0 {
			delete(m.taskUses, id)
		}
	}, nil
}

func (s *Store) managedWorkspaces() ([]managedWorkspace, error) {
	return s.workspacePage(context.Background(), "", 0)
}

func (s *Store) workspacePage(ctx context.Context, after string, limit int) ([]managedWorkspace, error) {
	query := "SELECT id,root,path,identity,state,cleanup_allowed,last_error FROM managed_workspaces WHERE id>? ORDER BY id"
	if limit > 0 {
		query += " LIMIT ?"
	}
	args := []any{after}
	if limit > 0 {
		args = append(args, limit)
	}
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []managedWorkspace{}
	for rows.Next() {
		var w managedWorkspace
		if err = rows.Scan(&w.ID, &w.Root, &w.Path, &w.Identity, &w.State, &w.CleanupAllowed, &w.LastError); err != nil {
			return nil, err
		}
		result = append(result, w)
	}
	return result, rows.Err()
}

func canonicalWorkspace(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	p, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	actual, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", err
	}
	return filepath.Clean(actual), nil
}

func pathsOverlap(a, b string) bool {
	if runtime.GOOS == "windows" {
		a, b = strings.ToLower(a), strings.ToLower(b)
	}
	return a != "" && b != "" && (inside(a, b) || inside(b, a))
}

// The Manager mutex owns usage and deletion admission, not file IO.
func (m *Manager) useWorkspace(task M) (func(), error) {
	path, err := canonicalWorkspace(S(task["workspace"]))
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.unlock()
	if m.closed || m.deleting[S(task["id"])] {
		return nil, Problem(409, "task_deleting", "任务正在关闭或删除")
	}
	if id := S(task["id"]); id != "" {
		if _, err := m.getLocked(id); err != nil {
			return nil, err
		}
	}
	for root := range m.workspaceDeleting {
		if pathsOverlap(path, root) {
			return nil, Problem(409, "workspace_cleanup", "目录正在回收")
		}
	}
	// Cleanup may have completed between path resolution and admission.
	if path != "" {
		if _, err := os.Stat(path); err != nil {
			return nil, err
		}
	}
	if m.workspaceUses == nil {
		m.workspaceUses = map[string]int{}
	}
	m.workspaceUses[path]++
	if m.taskUses == nil {
		m.taskUses = map[string]int{}
	}
	id := S(task["id"])
	m.taskUses[id]++
	return func() {
		m.mu.Lock()
		defer m.unlock()
		m.taskUses[id]--
		if m.taskUses[id] == 0 {
			delete(m.taskUses, id)
		}
		m.workspaceUses[path]--
		if m.workspaceUses[path] == 0 {
			delete(m.workspaceUses, path)
		}
	}, nil
}

// Record responsibility before exclusive creation. An unknown identity is
// retained for manual inspection; never adopt an existing directory.
func (m *Manager) allocateWorkspace(id string, allowed bool) (string, error) {
	root, err := canonicalWorkspace(m.options.WorkspaceRoot)
	if err != nil {
		return "", err
	}
	path := filepath.Join(root, id)
	err = m.options.Store.Write(func(tx *sql.Tx) error {
		_, e := tx.Exec("INSERT INTO managed_workspaces(id,root,path,state,cleanup_allowed) VALUES(?,?,?,'allocating',?)", id, root, path, allowed)
		return e
	})
	if err != nil {
		return "", err
	}
	if err = os.Mkdir(path, 0700); err != nil {
		m.workspaceError(id, err)
		return "", err
	}
	file, err := os.Open(path)
	if err != nil {
		m.workspaceError(id, err)
		return "", err
	}
	identity, err := directoryIdentity(file)
	file.Close()
	if err != nil {
		m.workspaceError(id, err)
		// The directory remains usable, but without a stable identity it must
		// never be reclaimed. Unsupported filesystems do not block conversations.
		return path, nil
	}
	err = m.options.Store.Write(func(tx *sql.Tx) error {
		_, e := tx.Exec("UPDATE managed_workspaces SET identity=? WHERE id=?", identity, id)
		return e
	})
	return path, err
}

func (m *Manager) workspaceError(id string, err error) {
	_ = m.options.Store.Write(func(tx *sql.Tx) error {
		_, e := tx.Exec("UPDATE managed_workspaces SET last_error=? WHERE id=?", lifecycleError(err), id)
		return e
	})
}

// Called in the task publication/deletion transaction. A retained independent
// fork keeps its FK and therefore keeps the shared directory attached.
func reconcileWorkspaceReferences(tx *sql.Tx) error {
	_, err := tx.Exec(`UPDATE managed_workspaces SET state=CASE
	 WHEN EXISTS(SELECT 1 FROM tasks WHERE managed_workspace_id=managed_workspaces.id) THEN 'attached'
	 WHEN state='attached' THEN 'pending' ELSE state END`)
	if err != nil {
		return err
	}
	// An unregistered-for-cleanup directory is permanently retained. Once its
	// last task goes away, no cleanup responsibility needs to remain in the DB.
	// This is independent of the global switch: opted-in pending rows survive.
	_, err = tx.Exec("DELETE FROM managed_workspaces WHERE state='pending' AND cleanup_allowed=0")
	return err
}

func (f *Files) cleanupWorkspaces(ctx context.Context) (bool, error) {
	m := f.Manager
	if !B(O(O(m.Config()["retention"])["managedWorkspaces"])["autoCleanup"]) {
		return false, nil
	}
	pending := false
	after := ""
	for {
		workspaces, err := m.options.Store.workspacePage(ctx, after, 32)
		if err != nil || len(workspaces) == 0 {
			return pending, err
		}
		for _, w := range workspaces {
			after = w.ID
			if err = ctx.Err(); err != nil {
				return pending, err
			}
			if !w.CleanupAllowed || w.State != "pending" {
				continue
			}
			if err = f.cleanupWorkspace(ctx, w); err != nil {
				pending = true
				m.workspaceError(w.ID, err)
			}
		}
	}
}

func (f *Files) cleanupWorkspace(ctx context.Context, w managedWorkspace) error {
	m := f.Manager
	if !publicID.MatchString(w.ID) || w.Path != filepath.Join(w.Root, w.ID) || w.Identity == "" {
		return Problem(409, "workspace_identity", "目录身份不完整，保留文件")
	}
	canonical, err := canonicalWorkspace(w.Root)
	sameRoot := canonical == w.Root
	if runtime.GOOS == "windows" {
		sameRoot = strings.EqualFold(canonical, w.Root)
	}
	if err != nil || !sameRoot {
		return Problem(409, "workspace_identity", "托管根目录路径已变化，保留文件")
	}
	m.mu.Lock()
	if m.closed || m.changing || !B(O(O(m.Config()["retention"])["managedWorkspaces"])["autoCleanup"]) {
		m.mu.Unlock()
		return Problem(409, "cleanup_paused", "回收已暂停")
	}
	// Read only reference facts, not every idle task's full projection.
	tasks, readErr := m.options.Store.workspaceReferences(ctx)
	if readErr != nil {
		m.mu.Unlock()
		return readErr
	}
	for _, task := range tasks {
		path := S(task["workspace"])
		// Missing directories still count as references; resolve aliases when possible.
		if actual, e := canonicalWorkspace(path); e == nil {
			path = actual
		}
		if S(task["managedWorkspaceId"]) == w.ID || pathsOverlap(path, w.Path) {
			m.mu.Unlock()
			return Problem(409, "workspace_referenced", "目录仍被对话引用")
		}
	}
	for path, count := range m.workspaceUses {
		if count > 0 && pathsOverlap(path, w.Path) {
			m.mu.Unlock()
			return Problem(409, "workspace_busy", "目录仍有在途操作")
		}
	}
	if m.workspaceDeleting == nil {
		m.workspaceDeleting = map[string]bool{}
	}
	if m.workspaceDeleting[w.Path] {
		m.mu.Unlock()
		return Problem(409, "workspace_busy", "目录正在回收")
	}
	m.workspaceDeleting[w.Path] = true
	m.mu.Unlock()
	defer func() { m.mu.Lock(); delete(m.workspaceDeleting, w.Path); m.mu.Unlock() }()
	root, err := os.OpenRoot(w.Root)
	if err != nil {
		return err
	}
	defer root.Close()
	info, err := root.Lstat(w.ID)
	if os.IsNotExist(err) {
		return m.forgetWorkspace(ctx, w.ID)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return Problem(409, "workspace_identity", "目录类型已变化，保留文件")
	}
	dir, err := root.OpenRoot(w.ID)
	if err != nil {
		return err
	}
	defer dir.Close()
	handle, err := dir.Open(".")
	if err != nil {
		return err
	}
	identity, err := directoryIdentity(handle)
	handle.Close()
	if err != nil {
		return err
	}
	actual, err := dir.Stat(".")
	if err != nil || !os.SameFile(info, actual) || identity != w.Identity {
		return Problem(409, "workspace_identity", "目录身份已变化，保留文件")
	}
	handle, err = dir.Open(".")
	if err != nil {
		return err
	}
	entries, err := handle.ReadDir(-1)
	handle.Close()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = dir.RemoveAll(entry.Name()); err != nil {
			return err
		}
	}
	// Remove only an empty root after checking identity again, never recursively
	// remove a path that may have been replaced after opening the anchored dir.
	current, err := root.Lstat(w.ID)
	if err != nil || !os.SameFile(info, current) {
		return Problem(409, "workspace_identity", "目录在回收期间变化")
	}
	dir.Close()
	if err = root.Remove(w.ID); err != nil {
		return err
	}
	return m.forgetWorkspace(ctx, w.ID)
}

func (s *Store) workspaceReferences(ctx context.Context) ([]M, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT COALESCE(managed_workspace_id,''), COALESCE(json_extract(payload_json,'$.workspace'),'') FROM tasks`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []M{}
	for rows.Next() {
		var id, path string
		if err = rows.Scan(&id, &path); err != nil {
			return nil, err
		}
		result = append(result, M{"managedWorkspaceId": id, "workspace": path})
	}
	return result, rows.Err()
}

func (m *Manager) forgetWorkspace(ctx context.Context, id string) error {
	return m.options.Store.WriteContext(ctx, func(tx *sql.Tx) error { _, e := tx.Exec("DELETE FROM managed_workspaces WHERE id=?", id); return e })
}
