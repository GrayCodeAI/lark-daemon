package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"lark-daemon/internal/proto"
)

// --- E2EE Key Registration ---

// handleRegisterKey registers a public key for the authenticated member.
func (r *Router) handleRegisterKey(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	var body struct {
		KeyType    string `json:"key_type"`
		PublicKey  string `json:"public_key"`
		PrivateKey string `json:"private_key,omitempty"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.KeyType == "" || body.PublicKey == "" {
		writeError(w, http.StatusBadRequest, "key_type and public_key required")
		return
	}
	validTypes := map[string]bool{"identity": true, "signed_pre": true, "one_time": true}
	if !validTypes[body.KeyType] {
		writeError(w, http.StatusBadRequest, "key_type must be identity, signed_pre, or one_time")
		return
	}
	k := &proto.UserKey{
		MemberID:   member.ID,
		KeyType:    proto.UserKeyType(body.KeyType),
		PublicKey:  body.PublicKey,
		PrivateKey: body.PrivateKey,
	}
	if err := r.services.RegisterUserKey(req.Context(), k); err != nil {
		serverError(w, err, "register key")
		return
	}
	writeJSON(w, http.StatusCreated, k)
}

// handleGetKeys returns public keys for a member (for key exchange).
func (r *Router) handleGetKeys(w http.ResponseWriter, req *http.Request) {
	memberID := chi.URLParam(req, "id")
	keyType := req.URL.Query().Get("type")
	if keyType == "" {
		writeError(w, http.StatusBadRequest, "type query param required")
		return
	}
	keys, err := r.services.GetUserKeys(req.Context(), memberID, proto.UserKeyType(keyType))
	if err != nil {
		serverError(w, err, "get keys")
		return
	}
	writeJSON(w, http.StatusOK, keys)
}

// handleDeleteKey deletes a specific key.
func (r *Router) handleDeleteKey(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	id := chi.URLParam(req, "id")
	key, err := r.services.GetUserKey(req.Context(), id)
	if err != nil {
		serverError(w, err, "get key")
		return
	}
	if key == nil {
		writeError(w, http.StatusNotFound, "key not found")
		return
	}
	if key.MemberID != member.ID {
		writeError(w, http.StatusForbidden, "not your key")
		return
	}
	if err := r.services.DeleteUserKey(req.Context(), id); err != nil {
		serverError(w, err, "delete key")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- E2EE Encrypted Messages ---

// handleSendEncrypted stores an encrypted message for a recipient.
func (r *Router) handleSendEncrypted(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	var body struct {
		MessageID         string `json:"message_id"`
		RecipientID       string `json:"recipient_id"`
		EncryptedContent  string `json:"encrypted_content"`
		SenderIdentityKey string `json:"sender_identity_key"`
		EphemeralKey      string `json:"ephemeral_key,omitempty"`
	}
	if err := decodeJSON(req, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.MessageID == "" || body.RecipientID == "" || body.EncryptedContent == "" || body.SenderIdentityKey == "" {
		writeError(w, http.StatusBadRequest, "message_id, recipient_id, encrypted_content, and sender_identity_key required")
		return
	}
	em := &proto.EncryptedMessage{
		MessageID:         body.MessageID,
		RecipientID:       body.RecipientID,
		EncryptedContent:  body.EncryptedContent,
		SenderIdentityKey: body.SenderIdentityKey,
		EphemeralKey:      body.EphemeralKey,
	}
	if err := r.services.CreateEncryptedMessage(req.Context(), em); err != nil {
		serverError(w, err, "create encrypted message")
		return
	}
	writeJSON(w, http.StatusCreated, em)
}

// handleGetEncrypted retrieves encrypted messages for the authenticated member.
func (r *Router) handleGetEncrypted(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	messageID := chi.URLParam(req, "id")
	msgs, err := r.services.GetEncryptedMessages(req.Context(), messageID, member.ID)
	if err != nil {
		serverError(w, err, "get encrypted messages")
		return
	}
	writeJSON(w, http.StatusOK, msgs)
}
