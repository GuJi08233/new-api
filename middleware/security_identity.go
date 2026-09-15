package middleware

import (
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

func CookieSecurityIdentity(c *gin.Context) (model.SecurityIdentity, error) {
	if _, ok := c.Get(sessions.DefaultKey); !ok {
		return model.SecurityIdentity{}, model.ErrSecurityIdentity
	}
	session := sessions.Default(c)
	id, ok := session.Get("id").(int)
	version, versionOK := session.Get("auth_version").(int64)
	sid, sidOK := session.Get("security_session").(string)
	if !ok || !versionOK || !sidOK || id <= 0 || len(sid) != 64 {
		return model.SecurityIdentity{}, model.ErrSecurityIdentity
	}
	return model.SecurityIdentity{UserID: id, Version: version, SessionID: sid}, nil
}

func CurrentCookieUser(c *gin.Context) (*model.User, error) {
	identity, err := CookieSecurityIdentity(c)
	if err != nil {
		return nil, err
	}
	return model.ReadSecurityIdentity(identity)
}
