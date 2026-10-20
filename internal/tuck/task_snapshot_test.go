package tuck

func (m *Manager) List() []M {
	tasks, err := m.Snapshots()
	if err != nil {
		panic(err)
	}
	return tasks
}

// Tests that mutate under the manager lock use the same lazy access as owners.
func fixtureTaskLocked(m *Manager, id string) M {
	task, err := m.getLocked(id)
	if err != nil {
		panic(err)
	}
	return task
}
