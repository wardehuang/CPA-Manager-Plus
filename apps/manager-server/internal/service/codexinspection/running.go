package codexinspection

// IsRunning reports whether a server-side Codex inspection task currently owns
// the process-local inspection lock.
func (s *Service) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}
