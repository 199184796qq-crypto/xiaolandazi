package httpapi

import "livecompanion/core/internal/messagecaptain"

func (s *Server) MessageCaptain() *messagecaptain.Captain { return s.messages }
