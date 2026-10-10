package agentws

// Revoke closes only sessions older than the committed credential generation.
// Generation zero closes all sessions (revocation or registration deletion).
func (h *Handler) Revoke(trapID string, generation int64) {
	entry := h.registry.entry(trapID)
	entry.mu.Lock()
	defer entry.mu.Unlock()
	session := entry.current
	if session == nil || generation != 0 && session.identity.CredentialGeneration >= generation {
		return
	}
	session.active.Store(false)
	_ = session.socket.CloseCode(4401, "authentication_revoked")
	session.cancel()
}
