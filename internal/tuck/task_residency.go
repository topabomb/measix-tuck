package tuck

// Only active task payloads belong in memory. Store row order remains the
// ordering authority; idle projections are read from it when needed.
// Call while holding mu, after finishing all mutations of borrowed task maps.
func (m *Manager) releaseIdleLocked() {
	for id, t := range m.tasks {
		busy := m.taskBusyLocked(t)
		seen := map[string]bool{id: true}
		for parent := S(t["parentTaskId"]); !busy && parent != "" && !seen[parent]; {
			seen[parent] = true
			ancestor := m.tasks[parent]
			if ancestor == nil {
				break
			}
			busy = m.taskBusyLocked(ancestor)
			parent = S(ancestor["parentTaskId"])
		}
		if !busy {
			delete(m.tasks, id)
			delete(m.normalizer.todos, id)
			delete(m.normalizer.activityTools, id)
			m.normalizer.timings.clear(id)
		}
	}
}

func (m *Manager) unlock() {
	m.releaseIdleLocked()
	m.mu.Unlock()
}

func (m *Manager) snapshotsLocked() ([]M, error) {
	tasks, err := m.options.Store.Tasks()
	if err != nil {
		return nil, err
	}
	for i, t := range tasks {
		if active := m.tasks[S(t["id"])]; active != nil {
			tasks[i] = Clone(active)
		}
	}
	return tasks, nil
}

func (m *Manager) Snapshots() ([]M, error) {
	m.mu.Lock()
	defer m.unlock()
	return m.snapshotsLocked()
}
