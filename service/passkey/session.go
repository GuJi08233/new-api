package passkey

import (
	"errors"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	webauthn "github.com/go-webauthn/webauthn/webauthn"
	"time"
)

type ceremonyPayload struct {
	Data        *webauthn.SessionData `json:"data"`
	Scope       string                `json:"scope"`
	ContextHash string                `json:"context_hash"`
}

func SaveSessionData(c *gin.Context, key string, data *webauthn.SessionData) error {
	session := sessions.Default(c)
	id, _ := session.Get("id").(int)
	version, _ := session.Get("auth_version").(int64)
	sid, _ := session.Get("security_session").(string)
	if key == LoginSessionKey {
		id, version, sid = 0, 0, ""
	}
	payload, err := common.Marshal(ceremonyPayload{Data: data, Scope: c.GetString("verification_scope"), ContextHash: c.GetString("verification_context")})
	if err != nil {
		return err
	}
	token, err := model.CreateSecurityFlow(model.SecurityFlow{UserID: id, Version: version, SessionID: sid, Kind: "passkey", Scope: key, Payload: string(payload), ExpiresAt: time.Now().Add(2 * time.Minute).Unix()})
	if err != nil {
		return err
	}
	session.Set(key, token)
	return session.Save()
}

func PopSessionData(c *gin.Context, key string) (*webauthn.SessionData, error) {
	session := sessions.Default(c)
	token, _ := session.Get(key).(string)
	id, _ := session.Get("id").(int)
	version, _ := session.Get("auth_version").(int64)
	sid, _ := session.Get("security_session").(string)
	if key == LoginSessionKey {
		id, version, sid = 0, 0, ""
	}
	flow, err := model.ConsumeSecurityFlow(token, "passkey", model.SecurityIdentity{UserID: id, Version: version, SessionID: sid}, key, "")
	if err != nil {
		return nil, err
	}
	session.Delete(key)
	if err := session.Save(); err != nil {
		return nil, err
	}
	var payload ceremonyPayload
	if err := common.UnmarshalJsonStr(flow.Payload, &payload); err != nil {
		return nil, err
	}
	if payload.Data == nil {
		return nil, errors.New("Passkey 会话无效")
	}
	c.Set("verification_scope", payload.Scope)
	c.Set("verification_context", payload.ContextHash)
	return payload.Data, nil
}
