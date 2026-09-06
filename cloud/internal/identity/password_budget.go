package identity

import "context"

// Reject excess work before allocating Argon2 memory. Every password operation
// shares this budget, including registration, reset and authenticated changes.
func (s *Service) acquirePasswordSlot(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case s.passwordSlots <- struct{}{}:
		return nil
	default:
		return ErrRateLimited
	}
}

func (s *Service) hashPassword(ctx context.Context, password string) (string, error) {
	if err := s.acquirePasswordSlot(ctx); err != nil {
		return "", err
	}
	defer func() { <-s.passwordSlots }()
	return s.passwords.Hash(password)
}

func (s *Service) verifyPassword(ctx context.Context, encoded, password string) (bool, error) {
	if err := s.acquirePasswordSlot(ctx); err != nil {
		return false, err
	}
	defer func() { <-s.passwordSlots }()
	return s.passwords.Verify(encoded, password)
}
