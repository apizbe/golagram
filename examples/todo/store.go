package main

import "sync"

// Task is one to-do item, scoped to whichever user created it.
type Task struct {
	ID   int
	Text string
	Done bool
}

// Store holds every user's tasks in memory. A real bot would swap this for
// a database or a JSON file (see golagram-test/internal/store for that
// shape) — the handlers below only ever talk to this interface-shaped
// struct, so nothing else would need to change.
type Store struct {
	mu     sync.Mutex
	tasks  map[int64][]Task
	nextID map[int64]int
}

func NewStore() *Store {
	return &Store{
		tasks:  make(map[int64][]Task),
		nextID: make(map[int64]int),
	}
}

// Add appends a new task for userID and returns it.
func (s *Store) Add(userID int64, text string) Task {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextID[userID]++
	t := Task{ID: s.nextID[userID], Text: text}
	s.tasks[userID] = append(s.tasks[userID], t)
	return t
}

// List returns userID's tasks, oldest first.
func (s *Store) List(userID int64) []Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Task(nil), s.tasks[userID]...)
}

// Toggle flips a task's done state. Reports whether the task existed.
func (s *Store) Toggle(userID int64, id int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, t := range s.tasks[userID] {
		if t.ID == id {
			s.tasks[userID][i].Done = !t.Done
			return true
		}
	}
	return false
}

// Delete removes a task. Reports whether it existed.
func (s *Store) Delete(userID int64, id int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.tasks[userID]
	for i, t := range list {
		if t.ID == id {
			s.tasks[userID] = append(list[:i], list[i+1:]...)
			return true
		}
	}
	return false
}

// ClearDone removes every completed task for userID and reports how many
// were removed.
func (s *Store) ClearDone(userID int64) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.tasks[userID][:0]
	removed := 0
	for _, t := range s.tasks[userID] {
		if t.Done {
			removed++
			continue
		}
		kept = append(kept, t)
	}
	s.tasks[userID] = kept
	return removed
}
