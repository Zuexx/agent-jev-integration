// Package store keeps notes in memory.
package store

import "sync"

type Note struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

type Store struct {
	mu    sync.Mutex
	notes map[int64]Note
}

func New(notes ...Note) *Store {
	s := &Store{notes: map[int64]Note{}}
	for _, n := range notes {
		s.notes[n.ID] = n
	}
	return s
}

func (s *Store) Get(id int64) (Note, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.notes[id]
	return n, ok
}

func (s *Store) List() []Note {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Note, 0, len(s.notes))
	for _, n := range s.notes {
		out = append(out, n)
	}
	return out
}
