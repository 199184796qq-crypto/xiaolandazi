package httpapi

import (
	"context"
	"fmt"
	"io"
)

func (s *Server) syncInteractionPreferencesForStart(ctx context.Context, tenant, room int64) error {
	prefs, err := s.store.GetRoomInteractionPreferences(ctx, tenant, room)
	if err != nil {
		return err
	}
	if s.core == nil {
		return fmt.Errorf("Core unavailable")
	}
	resp, err := s.core.PutRoomInteractionPreferences(ctx, prefs)
	if err != nil {
		return err
	}
	if resp == nil {
		return fmt.Errorf("Core empty response")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 8192))
		return fmt.Errorf("Core preferences HTTP %d", resp.StatusCode)
	}
	return nil
}
